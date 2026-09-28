// Package vendor drives the reference keypad's vendor-specific HID
// interface (UsagePage 0xFF00): the command/response channel the
// manufacturer's configurator uses to remap inputs and set the RGB.
//
// The wire format is documented in docs/hid-device-keyforge-keypad.md
// §4.3. Every packet is built here from a fixed set of commands; the
// destructive ones (factory reset, bootloader entry) are deliberately
// not implemented, and no raw-send escape hatch is exported.
package vendor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sstallion/go-hid"
)

const (
	reportLength = 64

	commandClass byte = 0x06
	ackMarker    byte = 0xAA

	cmdReadSlots   byte = 0x08
	cmdWriteSlot   byte = 0x10
	cmdSetEffect   byte = 0x0B
	cmdSetKeyColor byte = 0x14

	// Reads are acknowledged with 0x07 rather than echoing 0x08.
	ackReadSlots byte = 0x07

	// The byte after each command is a length field copied verbatim
	// from the vendor configurator; it does not match the data size
	// (reads ask for 0x3A but return 56 bytes), so it is not derived.
	lenReadSlots   byte = 0x3A
	lenWriteSlot   byte = 0x07
	lenSetEffect   byte = 0x0B
	lenSetKeyColor byte = 0x03

	// Only layer 0 has been verified on hardware.
	layer byte = 0x00

	slotSize      = 4
	slotChunkSize = 56
	slotDataStart = 8

	// Replies echo the host packet from byte 3 on; reads only echo the
	// 16-bit offset before their data.
	echoStart   = 3
	readEchoLen = 2

	defaultAckTimeout = 500 * time.Millisecond
	pollTimeout       = 50 * time.Millisecond
)

// Limits bounds the indices a client accepts. Writes persist on the
// device and are acked even past the wired range, so anything outside
// the verified map is rejected up front.
type Limits struct {
	// Slots is how many input slots back a physical input.
	Slots int
	// LEDs is how many keys have an addressable LED.
	LEDs int
}

// transport is the subset of *hid.Device the client needs, isolated so
// tests can run without hardware.
type transport interface {
	Write(p []byte) (int, error)
	ReadWithTimeout(p []byte, timeout time.Duration) (int, error)
	Close() error
}

// Client sends commands over an opened vendor interface. It is not safe
// for concurrent use: every command is a write followed by its ack.
//
// Every command takes a ctx that is checked before the write and on each
// ack poll, so cancellation is noticed within pollTimeout. A Write or
// open blocked inside hidapi cannot be interrupted; ctx is honored once
// it returns.
type Client struct {
	t          transport
	limits     Limits
	ackTimeout time.Duration
}

// Open opens the vendor interface at the given platform path (the Path
// of the Info whose UsagePage is 0xFF00) with openPath. Pass
// device.OpenPath: it serializes the open with the rest of the
// process's hidapi use, which this package cannot import.
func Open(path string, limits Limits, openPath func(string) (*hid.Device, error)) (*Client, error) {
	return open(path, limits, func(p string) (transport, error) { return openPath(p) })
}

func open(path string, limits Limits, openPath func(string) (transport, error)) (*Client, error) {
	t, err := openPath(path)
	if err != nil {
		return nil, fmt.Errorf("vendor.Open %q: %w: %w", path, ErrOpen, err)
	}
	return &Client{t: t, limits: limits, ackTimeout: defaultAckTimeout}, nil
}

// Close releases the underlying transport.
func (c *Client) Close() error {
	if err := c.t.Close(); err != nil {
		return fmt.Errorf("vendor.Close: %w: %w", ErrClose, err)
	}
	return nil
}

// ReadSlots returns every input slot within the client's limits.
func (c *Client) ReadSlots(ctx context.Context) ([]Slot, error) {
	size := c.limits.Slots * slotSize
	data := make([]byte, 0, size)
	for off := 0; off < size; off += slotChunkSize {
		reply, err := c.exchange(ctx, ackReadSlots, readEchoLen,
			cmdReadSlots, lenReadSlots, byte(off), byte(off>>8), 0x00, layer)
		if err != nil {
			return nil, fmt.Errorf("vendor.ReadSlots: %w", err)
		}
		data = append(data, reply[slotDataStart:]...)
	}

	slots := make([]Slot, c.limits.Slots)
	for i := range slots {
		b := data[i*slotSize:]
		slots[i] = Slot{Type: SlotType(b[0]), Codes: [3]byte{b[1], b[2], b[3]}}
	}
	return slots, nil
}

// WriteSlot stores s in the given input slot. The change takes effect
// immediately and persists on the device.
func (c *Client) WriteSlot(ctx context.Context, index int, s Slot) error {
	if index < 0 || index >= c.limits.Slots {
		return fmt.Errorf("vendor.WriteSlot (index %d, limit %d): %w", index, c.limits.Slots, ErrInvalidArgument)
	}
	off := index * slotSize
	payload := []byte{cmdWriteSlot, lenWriteSlot, byte(off), byte(off >> 8), 0x00, layer, 0x00,
		byte(s.Type), s.Codes[0], s.Codes[1], s.Codes[2]}
	if _, err := c.exchange(ctx, cmdWriteSlot, len(payload)-2, payload...); err != nil {
		return fmt.Errorf("vendor.WriteSlot: %w", err)
	}
	return nil
}

// ApplySlots makes the device's input slots match want (one entry per
// slot, index = slot) and returns how many it had to write. Slots that
// already match are left alone, so applying the same layout twice
// writes nothing the second time.
func (c *Client) ApplySlots(ctx context.Context, want []Slot) (int, error) {
	if len(want) != c.limits.Slots {
		return 0, fmt.Errorf("vendor.ApplySlots (got %d slots, want %d): %w", len(want), c.limits.Slots, ErrInvalidArgument)
	}
	current, err := c.ReadSlots(ctx)
	if err != nil {
		return 0, fmt.Errorf("vendor.ApplySlots: %w", err)
	}
	written := 0
	for i, slot := range want {
		if current[i] == slot {
			continue
		}
		if err := c.WriteSlot(ctx, i, slot); err != nil {
			return written, fmt.Errorf("vendor.ApplySlots (slot %d): %w", i, err)
		}
		written++
	}
	return written, nil
}

// SetEffect replaces the keypad-wide lighting effect.
func (c *Client) SetEffect(ctx context.Context, e Effect) error {
	mode := byte(0x02)
	if e.Mono {
		mode = 0x03
	}
	payload := []byte{cmdSetEffect, lenSetEffect, 0x00, 0x00, 0x01, 0x00,
		byte(e.Style), e.Speed, mode, 0x01, 0x01, 0x00, e.Color.H, e.Color.S, e.Color.V}
	if _, err := c.exchange(ctx, cmdSetEffect, len(payload)-2, payload...); err != nil {
		return fmt.Errorf("vendor.SetEffect: %w", err)
	}
	return nil
}

// SetKeyColor sets one key's LED. The color only shows while the effect
// style is StyleUserLight.
func (c *Client) SetKeyColor(ctx context.Context, led int, color RGB) error {
	if led < 0 || led >= c.limits.LEDs {
		return fmt.Errorf("vendor.SetKeyColor (led %d, limit %d): %w", led, c.limits.LEDs, ErrInvalidArgument)
	}
	payload := []byte{cmdSetKeyColor, lenSetKeyColor, byte(led * 3), 0x00, 0x00, 0x00, 0x00,
		color.R, color.G, color.B}
	if _, err := c.exchange(ctx, cmdSetKeyColor, len(payload)-2, payload...); err != nil {
		return fmt.Errorf("vendor.SetKeyColor: %w", err)
	}
	return nil
}

// exchange writes one command and waits for its ack. A report only
// counts as the ack when it carries ackCmd and echoes the first echoLen
// bytes of the payload after the length field, so a late ack left over
// from an earlier timed-out command is skipped rather than taken as
// confirmation of this one. It returns ctx.Err() (wrapped) once ctx is
// done, before writing or between ack polls.
func (c *Client) exchange(ctx context.Context, ackCmd byte, echoLen int, payload ...byte) ([]byte, error) {
	// The interface has no report IDs, so hidapi expects a leading 0x00.
	out := make([]byte, 1+reportLength)
	out[1] = commandClass
	copy(out[2:], payload)
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("command 0x%02x: %w", payload[0], err)
	}
	if _, err := c.t.Write(out); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrWrite, err)
	}
	echo := payload[2 : 2+echoLen]

	in := make([]byte, reportLength)
	deadline := time.Now().Add(c.ackTimeout)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("command 0x%02x: %w", payload[0], err)
		}
		n, err := c.t.ReadWithTimeout(in, pollTimeout)
		if errors.Is(err, hid.ErrTimeout) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrRead, err)
		}
		if n == reportLength && in[0] == ackMarker && in[1] == ackCmd &&
			bytes.Equal(in[echoStart:echoStart+echoLen], echo) {
			return in, nil
		}
	}
	// ctx may expire during the last poll; report it rather than ErrNoAck
	// so callers can tell their own timeout from a silent device.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("command 0x%02x: %w", payload[0], err)
	}
	return nil, fmt.Errorf("command 0x%02x: %w", payload[0], ErrNoAck)
}

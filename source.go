// Package hid is the public entry point to keyforge-hid. It discovers the
// HID keypad the built-in registry recognizes and streams its decoded
// input reports as protocol.InputEvent values, wrapping the enumeration /
// identification / decoding pipeline that lives under internal/ so other
// modules (notably keyforge-core) can consume hardware events without
// reaching into internal packages.
package hid

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	"github.com/JoniDG/keyforge-hid/internal/device"
	"github.com/JoniDG/keyforge-hid/internal/events"
	"github.com/JoniDG/keyforge-hid/internal/vendor"
	"github.com/JoniDG/keyforge-protocol/go/protocol"
)

// ErrNoRecognizedDevice is returned by Discover and Stream when no
// connected HID device matches the built-in registry (e.g. the keypad is
// unplugged). Callers can branch on it with errors.Is.
var ErrNoRecognizedDevice = errors.New("hid: no recognized device connected")

// ErrNoVendorInterface is returned by Provision when the recognized
// device has no vendor interface KeyForge can configure, or it was not
// enumerated.
var ErrNoVendorInterface = errors.New("hid: recognized device has no vendor interface")

// ErrInputNotRGB is returned (wrapped) by PaintInputs when a requested
// input id is not in the device's catalog or has no LED.
var ErrInputNotRGB = errors.New("hid: input has no LED")

// ErrInvalidColor is returned (wrapped) by PaintInputs when a color is
// not a '#RRGGBB' hex string.
var ErrInvalidColor = errors.New("hid: invalid color")

// Failures talking to the vendor interface, returned wrapped by Provision
// and PaintInputs so callers can tell them apart with errors.Is.
var (
	ErrVendorOpen  = vendor.ErrOpen
	ErrVendorWrite = vendor.ErrWrite
	ErrVendorRead  = vendor.ErrRead
	ErrVendorNoAck = vendor.ErrNoAck
)

// ErrSeizeNotImplemented is returned (wrapped) by Stream on a Source built
// with WithSeize(true) when the current platform's seize implementation is
// still a stub. Callers can branch on it with errors.Is and retry with a
// shared-mode Source instead of treating it as a device failure.
var ErrSeizeNotImplemented = device.ErrSeizeNotImplemented

// SeizeSupport reports whether the current build accepts WithSeize(true)
// (Stream does not fail with ErrSeizeNotImplemented) and a one-line
// description of the platform's seize status, suitable for logs or UI.
// note is never empty.
func SeizeSupport() (supported bool, note string) {
	s := device.PlatformSeizeSupport()
	return s.Supported, s.Note
}

// Device identifies the recognized keypad a Source streams from. ID is
// the protocol.DeviceID every InputEvent delivered by Stream carries, so
// callers can build bindings keyed on it before streaming starts.
type Device struct {
	ID   protocol.DeviceID
	Name string
}

// Source discovers the recognized keypad and streams its input events.
// Build one with New; the zero value is not usable.
type Source struct {
	enumerator device.Enumerator
	identifier device.Identifier
	opener     device.Opener
	setSeize   func(enabled bool) error
	openVendor func(path string, limits vendor.Limits) (vendorClient, error)
	// vendorSem holds one token while Provision or PaintInputs talk to
	// the vendor interface: a command and its ack must not interleave
	// with another one.
	vendorSem chan struct{}
	seize     bool
}

// vendorClient is the part of *vendor.Client that Provision and
// PaintInputs need.
type vendorClient interface {
	ApplySlots(ctx context.Context, want []vendor.Slot) (int, error)
	SetKeyColor(ctx context.Context, led int, color vendor.RGB) error
	SetEffect(ctx context.Context, e vendor.Effect) error
	Close() error
}

// Option customizes a Source built by New.
type Option func(*Source)

// WithSeize controls whether the Source takes exclusive control of the
// keypad so the OS stops receiving its reports in parallel.
//
// Seizing needs elevated privileges on some platforms (sudo / Input
// Monitoring entitlements on macOS, udev rules or root on Linux), so the
// default is false: the OS keeps receiving reports and no privileges are
// required — convenient for development. Pass WithSeize(true) to take
// over the device; Stream then fails if the current platform cannot
// honor the request rather than silently degrading to shared mode.
func WithSeize(seize bool) Option {
	return func(s *Source) { s.seize = seize }
}

// New builds a Source backed by the real HID pipeline (hidapi-backed
// enumerator and opener, the default device registry).
func New(opts ...Option) *Source {
	s := &Source{
		enumerator: device.NewEnumerator(),
		identifier: device.NewIdentifier(device.DefaultRegistry()),
		opener:     device.NewOpener(),
		setSeize:   device.SetSeize,
		openVendor: func(path string, limits vendor.Limits) (vendorClient, error) {
			return vendor.Open(path, limits, device.OpenPath)
		},
		vendorSem: make(chan struct{}, 1),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Discover enumerates connected HID devices and returns the first one the
// registry recognizes, or ErrNoRecognizedDevice when none is connected.
// The returned Device.ID equals the DeviceId every InputEvent from Stream
// will carry, so callers can resolve bindings before streaming.
func (s *Source) Discover() (Device, error) {
	r, err := s.resolve(context.Background())
	if err != nil {
		return Device{}, err
	}
	return Device{ID: r.id, Name: r.target.Known.Name}, nil
}

// DiscoverDevice enumerates connected HID devices and returns the first
// one the registry recognizes as a fully populated protocol.Device, or
// ErrNoRecognizedDevice when none is connected. Unlike Discover it does
// not require streaming, so callers (notably keyforge-core's daemon) can
// persist the device and serve its catalog at startup without opening it.
//
// The returned Device.Id matches the one Discover reports and every
// InputEvent Stream carries; Inputs is the device's curated logical input
// catalog. VendorId/ProductId are lowercase 4-digit hex with no 0x prefix.
//
// Inputs describes the keypad after Provision: until it runs, the keypad
// emits its factory codes, which are not in the catalog.
func (s *Source) DiscoverDevice() (protocol.Device, error) {
	r, err := s.resolve(context.Background())
	if err != nil {
		return protocol.Device{}, err
	}
	return toProtocolDevice(r.target, r.id), nil
}

// Stream discovers the recognized keypad and delivers its decoded input
// events to sink until ctx is cancelled, sink returns an error, or a
// reader fails. It returns nil on clean ctx cancellation, propagates the
// error from sink or a failing reader otherwise, and returns
// ErrNoRecognizedDevice when no recognized device is connected.
//
// sink is invoked from a single goroutine in event arrival order; a
// non-nil return stops every reader and propagates out of Stream.
func (s *Source) Stream(ctx context.Context, sink func(protocol.InputEvent) error) error {
	r, err := s.resolve(ctx)
	if err != nil {
		return err
	}
	if err := s.setSeize(s.seize); err != nil {
		return fmt.Errorf("hid.Stream: %w", err)
	}
	err = events.StreamAll(ctx, s.opener, r.id, r.inputs, sink)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("hid.Stream: %w", err)
	}
	return nil
}

// Provision writes the KeyForge input layout to the recognized keypad
// so every key and encoder emits a distinct code, matching the input
// catalog DiscoverDevice reports. The layout persists on the device and
// only slots that differ are written, so calling Provision on an
// already provisioned keypad changes nothing. It returns
// ErrNoRecognizedDevice when no recognized device is connected and
// ErrNoVendorInterface when the device cannot be configured. On any
// other error, cancellation included, the keypad may be partially
// provisioned; calling Provision again converges.
//
// Provision opens the vendor interface, which is separate from the ones
// Stream reads. On the reference keypad under macOS it needs no elevated
// privileges and works while Stream is running; other platforms are not
// verified yet. Calls to Provision and PaintInputs on the same Source run
// one at a time.
//
// ctx bounds the whole call: once it is cancelled or its deadline passes,
// Provision stops talking to the keypad, closes the vendor interface and
// returns ctx.Err() wrapped, so errors.Is(err, context.DeadlineExceeded)
// holds. Cancellation is checked while waiting for another vendor call,
// between commands and while waiting for each ack; a write or open
// blocked inside hidapi cannot be interrupted, so ctx is honored only
// once that call returns.
func (s *Source) Provision(ctx context.Context) error {
	r, path, err := s.resolveVendor(ctx)
	if err != nil {
		return err
	}
	spec := r.target.Known.Vendor
	return s.withVendor(ctx, "hid.Provision", path, spec, func(c vendorClient) error {
		_, err := c.ApplySlots(ctx, spec.Layout)
		return err
	})
}

// userLightEffect is the effect under which the per-key colors show.
// Speed is irrelevant to it; 2 matches the vendor configurator's default.
var userLightEffect = vendor.Effect{Style: vendor.StyleUserLight, Speed: 2}

// PaintInputs sets the LED of exactly the inputs in colors (input id →
// '#RRGGBB', any case; '#000000' turns the LED off) on the recognized
// keypad and switches it to the per-key lighting effect those colors
// need. Inputs left out keep whatever color they had. Colors are
// gamma-corrected for the device's LEDs (vendor.RGB.Corrected) so they
// look closer to the same '#RRGGBB' on a screen.
//
// Every entry is checked before anything is written: an id that is not
// in the catalog DiscoverDevice reports, or whose input has no LED
// (Input.Rgb unset), fails with ErrInputNotRGB, and a malformed color
// with ErrInvalidColor. It returns ErrNoRecognizedDevice and
// ErrNoVendorInterface like Provision, and wraps vendor failures so
// errors.Is matches ErrVendorOpen, ErrVendorWrite, ErrVendorRead or
// ErrVendorNoAck.
//
// Colors are written first and the effect last. On any error after the
// first write, cancellation included, some LEDs may already show their
// new color; calling PaintInputs again converges. Like Provision, it
// works while Stream is running, runs one at a time with Provision on the
// same Source, and honors ctx between commands and while waiting for
// each ack (up to 500 ms per command).
func (s *Source) PaintInputs(ctx context.Context, colors map[string]protocol.Color) error {
	r, path, err := s.resolveVendor(ctx)
	if err != nil {
		return err
	}
	spec := r.target.Known.Vendor
	paints, err := ledPaints(spec.LEDs, spec.Gamma, colors)
	if err != nil {
		return fmt.Errorf("hid.PaintInputs: %w", err)
	}
	return s.withVendor(ctx, "hid.PaintInputs", path, spec, func(c vendorClient) error {
		for _, p := range paints {
			if err := c.SetKeyColor(ctx, p.led, p.color); err != nil {
				return err
			}
		}
		return c.SetEffect(ctx, userLightEffect)
	})
}

type ledPaint struct {
	led   int
	color vendor.RGB
}

// ledPaints validates colors against the device's LED map and returns
// them gamma-corrected for its LEDs, in LED order so the writes are
// deterministic.
func ledPaints(leds []string, gamma float64, colors map[string]protocol.Color) ([]ledPaint, error) {
	index := make(map[string]int, len(leds))
	for i, id := range leds {
		index[id] = i
	}
	// Sorted so the first invalid entry reported does not depend on map
	// iteration order.
	ids := make([]string, 0, len(colors))
	for id := range colors {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	paints := make([]ledPaint, 0, len(colors))
	for _, id := range ids {
		led, ok := index[id]
		if !ok {
			return nil, fmt.Errorf("input %q: %w", id, ErrInputNotRGB)
		}
		rgb, err := parseColor(colors[id])
		if err != nil {
			return nil, fmt.Errorf("input %q: %w", id, err)
		}
		paints = append(paints, ledPaint{led: led, color: rgb.Corrected(gamma)})
	}
	sort.Slice(paints, func(i, j int) bool { return paints[i].led < paints[j].led })
	return paints, nil
}

// parseColor decodes a '#RRGGBB' protocol.Color in either case.
func parseColor(c protocol.Color) (vendor.RGB, error) {
	s := string(c)
	if len(s) != 7 || s[0] != '#' {
		return vendor.RGB{}, fmt.Errorf("%q: %w", s, ErrInvalidColor)
	}
	b, err := hex.DecodeString(s[1:])
	if err != nil {
		return vendor.RGB{}, fmt.Errorf("%q: %w", s, ErrInvalidColor)
	}
	return vendor.RGB{R: b[0], G: b[1], B: b[2]}, nil
}

// resolveVendor resolves the recognized device and the path of its
// vendor interface.
func (s *Source) resolveVendor(ctx context.Context) (resolved, string, error) {
	r, err := s.resolve(ctx)
	if err != nil {
		return resolved{}, "", err
	}
	iface, ok := r.target.VendorInfo()
	if !ok {
		return resolved{}, "", ErrNoVendorInterface
	}
	return r, iface.Path, nil
}

// withVendor runs fn over the vendor interface at path, holding vendorSem
// for the whole session and closing the interface afterwards. Errors are
// wrapped with op.
func (s *Source) withVendor(ctx context.Context, op, path string, spec device.VendorInterface, fn func(vendorClient) error) error {
	// Checked up front because select picks at random when both cases
	// are ready; a ctx that ends while waiting is caught by select.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	select {
	case s.vendorSem <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", op, ctx.Err())
	}
	defer func() { <-s.vendorSem }()
	client, err := s.openVendor(path, vendor.Limits{Slots: spec.Slots, LEDs: len(spec.LEDs)})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if err := errors.Join(fn(client), client.Close()); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

// resolved bundles the recognized device with the DeviceID derived from
// it and the decodable inputs that back its event stream, so Discover,
// DiscoverDevice and Stream all agree on the same identity.
type resolved struct {
	target device.IdentifiedDevice
	id     protocol.DeviceID
	inputs []device.MatchedInput
}

func (s *Source) resolve(ctx context.Context) (resolved, error) {
	infos, err := s.enumerator.List(ctx)
	if err != nil {
		return resolved{}, fmt.Errorf("hid.resolve: %w", err)
	}
	target, ok := firstRecognized(s.identifier.Identify(infos))
	if !ok {
		return resolved{}, ErrNoRecognizedDevice
	}
	// A recognized device always has at least one enumerated interface
	// (Identify builds it from one), and a USB device's iSerialNumber is
	// shared across all its interfaces, so Interfaces[0] is a safe and
	// stable seed for the DeviceID that both Discover and Stream agree on.
	deviceID := events.DeviceIDFor(target.VendorID, target.ProductID, target.Interfaces[0].Serial)
	return resolved{
		target: target,
		id:     deviceID,
		inputs: target.Inputs(),
	}, nil
}

// toProtocolDevice maps a recognized device to the protocol.Device shape
// keyforge-core persists and serves. VendorID/ProductID become lowercase
// 4-digit hex (the schema's ^[0-9A-Fa-f]{4}$ form); Path, Manufacturer,
// Product and Serial come from Interfaces[0], the same interface that
// seeds the DeviceID. The optional descriptor strings are omitted when
// empty (cheap clones often leave them blank, and Serial is unset until a
// stream starts on macOS). Inputs is the device's curated logical catalog.
func toProtocolDevice(d device.IdentifiedDevice, id protocol.DeviceID) protocol.Device {
	iface := d.Interfaces[0]
	return protocol.Device{
		Id:           id,
		VendorId:     fmt.Sprintf("%04x", d.VendorID),
		ProductId:    fmt.Sprintf("%04x", d.ProductID),
		Path:         iface.Path,
		Inputs:       copyInputs(d.Known.Controls),
		Manufacturer: optionalString(iface.Manufacturer),
		Product:      optionalString(iface.Product),
		SerialNumber: optionalString(iface.Serial),
	}
}

// copyInputs deep-copies the catalog entries so a client mutating the
// result (including through Label, Rgb or Layout) can't disturb the
// shared registry entry, and the required "inputs" field always marshals
// as an array rather than null.
func copyInputs(src []protocol.Input) []protocol.Input {
	out := make([]protocol.Input, len(src))
	for i, in := range src {
		out[i] = in
		out[i].Label = clonePtr(in.Label)
		out[i].Rgb = clonePtr(in.Rgb)
		out[i].Layout = clonePtr(in.Layout)
		if in.Layout != nil {
			out[i].Layout.W = clonePtr(in.Layout.W)
			out[i].Layout.H = clonePtr(in.Layout.H)
		}
	}
	return out
}

func clonePtr[T float64 | bool | string | protocol.InputLayout](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// optionalString returns a pointer to s, or nil when s is empty, matching
// the omitempty optional descriptor fields on protocol.Device.
func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func firstRecognized(devices []device.IdentifiedDevice) (device.IdentifiedDevice, bool) {
	for _, d := range devices {
		if d.Recognized {
			return d, true
		}
	}
	return device.IdentifiedDevice{}, false
}

// Package hid is the public entry point to keyforge-hid. It discovers the
// HID keypad the built-in registry recognizes and streams its decoded
// input reports as protocol.InputEvent values, wrapping the enumeration /
// identification / decoding pipeline that lives under internal/ so other
// modules (notably keyforge-core) can consume hardware events without
// reaching into internal packages.
package hid

import (
	"context"
	"errors"
	"fmt"

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
	openVendor func(path string, limits vendor.Limits) (slotApplier, error)
	seize      bool
}

// slotApplier is the part of *vendor.Client that Provision needs.
type slotApplier interface {
	ApplySlots(ctx context.Context, want []vendor.Slot) (int, error)
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
		openVendor: func(path string, limits vendor.Limits) (slotApplier, error) {
			return vendor.Open(path, limits)
		},
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
// verified yet.
//
// ctx bounds the whole call: once it is cancelled or its deadline passes,
// Provision stops talking to the keypad, closes the vendor interface and
// returns ctx.Err() wrapped, so errors.Is(err, context.DeadlineExceeded)
// holds. Cancellation is checked between commands and while waiting for
// each ack; a write or open blocked inside hidapi cannot be interrupted,
// so ctx is honored only once that call returns.
func (s *Source) Provision(ctx context.Context) error {
	r, err := s.resolve(ctx)
	if err != nil {
		return err
	}
	iface, ok := r.target.VendorInfo()
	if !ok {
		return ErrNoVendorInterface
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("hid.Provision: %w", err)
	}
	spec := r.target.Known.Vendor
	client, err := s.openVendor(iface.Path, vendor.Limits{Slots: spec.Slots, LEDs: spec.LEDs})
	if err != nil {
		return fmt.Errorf("hid.Provision: %w", err)
	}
	_, applyErr := client.ApplySlots(ctx, spec.Layout)
	if err := errors.Join(applyErr, client.Close()); err != nil {
		return fmt.Errorf("hid.Provision: %w", err)
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

// copyInputs returns a fresh non-nil slice over the catalog entries:
// mutating the returned slice (append, or reassigning an entry's value
// fields) can't disturb the shared registry entry, and the required
// "inputs" field always marshals as an array rather than null.
func copyInputs(src []protocol.Input) []protocol.Input {
	out := make([]protocol.Input, len(src))
	copy(out, src)
	return out
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

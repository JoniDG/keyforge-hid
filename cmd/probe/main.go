// Command probe lists HID devices visible to the host. With -stream
// it opens the first recognized device's input interfaces and dumps
// activity until interrupted.
//
// Default mode with -events: opens every interface declared by the
// device's registry entry in parallel and emits decoded
// protocol.InputEvent JSON lines (keyboard + encoder, merged into one
// output stream).
//
// Single-interface mode (with -usage or -path): opens just that one
// interface — hex dump by default, JSON events with -events. Useful
// for debugging unrecognized layouts.
//
// Vendor mode (-vendor-slots, -vendor-effect, -vendor-color,
// -provision, -factory-layout) talks to the recognized device's
// vendor-specific interface instead: it reads or rewrites the input
// slots, or changes the lighting. It does not need sudo on macOS.
//
// Probe asks the platform to seize HID devices by default so the OS
// stops receiving their reports in parallel; pass -shared to keep
// the legacy behavior (e.g. to verify that a keypad's keystrokes
// reach the OS in parallel with our decoder).
//
// This is a development helper for KeyForge phase 1; not part of the
// production binaries.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/JoniDG/keyforge-hid/internal/device"
	"github.com/JoniDG/keyforge-hid/internal/events"
	"github.com/JoniDG/keyforge-hid/internal/vendor"
	"github.com/JoniDG/keyforge-protocol/go/protocol"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	stream := fs.Bool("stream", false, "open the first recognized device's primary input interface and dump reports")
	emitEvents := fs.Bool("events", false, "with -stream, decode reports into protocol.InputEvent JSON lines instead of hex dump")
	usageSelector := fs.String("usage", "", "with -stream, target the interface whose UsagePage:Usage equals AAAA:BBBB (hex, no 0x). Defaults to the primary keyboard interface.")
	pathSelector := fs.String("path", "", "with -stream, target the interface whose platform path equals this exact value. Use when two interfaces share a usage. Mutually exclusive with -usage.")
	shared := fs.Bool("shared", false, "with -stream, do NOT seize HID devices; let the OS receive reports in parallel. Default is to seize on supported platforms.")
	vendorSlots := fs.Bool("vendor-slots", false, "read the recognized device's input slots (layer 0) over its vendor interface")
	vendorEffect := fs.String("vendor-effect", "", "set the recognized device's lighting effect: off|static|breath|trigger|spectrum|user")
	vendorColor := fs.String("vendor-color", "", "set one key LED as LED:RRGGBB (switches the effect to user light first)")
	provision := fs.Bool("provision", false, "write the KeyForge input layout so every key and encoder emits a distinct code")
	factoryLayout := fs.Bool("factory-layout", false, "write the factory input layout back (undoes -provision)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("probe: %w", err)
	}
	if *emitEvents && !*stream {
		return errors.New("probe: -events requires -stream")
	}
	if *usageSelector != "" && !*stream {
		return errors.New("probe: -usage requires -stream")
	}
	if *pathSelector != "" && !*stream {
		return errors.New("probe: -path requires -stream")
	}
	if *pathSelector != "" && *usageSelector != "" {
		return errors.New("probe: -path and -usage are mutually exclusive")
	}
	if *shared && !*stream {
		return errors.New("probe: -shared requires -stream")
	}
	vendorMode := *vendorSlots || *vendorEffect != "" || *vendorColor != "" || *provision || *factoryLayout
	if vendorMode && *stream {
		return errors.New("probe: -vendor-* flags and -stream are mutually exclusive")
	}
	if *vendorEffect != "" && *vendorColor != "" {
		return errors.New("probe: -vendor-effect and -vendor-color are mutually exclusive (-vendor-color switches the effect to user light)")
	}
	if *provision && *factoryLayout {
		return errors.New("probe: -provision and -factory-layout are mutually exclusive")
	}
	var lighting vendorLighting
	if *vendorEffect != "" {
		style, ok := effectStyles[*vendorEffect]
		if !ok {
			return fmt.Errorf("probe: unknown effect %q", *vendorEffect)
		}
		lighting.effect = &vendor.Effect{Style: style, Speed: 2, Color: vendor.HSV{S: 0xFF, V: 0xFF}}
	}
	if *vendorColor != "" {
		led, rgb, err := parseLEDColor(*vendorColor)
		if err != nil {
			return fmt.Errorf("probe: %w", err)
		}
		lighting.effect = &vendor.Effect{Style: vendor.StyleUserLight, Speed: 2}
		lighting.led, lighting.color = &led, &rgb
	}

	infos, err := device.NewEnumerator().List(context.Background())
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}

	identified := device.NewIdentifier(device.DefaultRegistry()).Identify(infos)

	if vendorMode {
		return runVendor(identified, vendorRequest{readSlots: *vendorSlots, provision: *provision, factoryLayout: *factoryLayout, lighting: lighting})
	}
	if *stream {
		seizeStatus := resolveSeize(!*shared, os.Stderr)
		return streamFirstRecognized(identified, *emitEvents, *usageSelector, *pathSelector, seizeStatus)
	}
	return listAll(infos, identified)
}

// seizeStatus captures what actually happened when probe tried (or
// chose not) to seize the keypad, so the banner can report it.
type seizeStatus struct {
	requested bool
	active    bool
	note      string
}

func (s seizeStatus) String() string {
	switch {
	case !s.requested:
		return "Seized: no (--shared override)"
	case s.active:
		return "Seized: yes (" + s.note + ")"
	default:
		return "Seized: no (" + s.note + ")"
	}
}

func resolveSeize(want bool, warn io.Writer) seizeStatus {
	support := device.PlatformSeizeSupport()
	err := device.SetSeize(want)
	active := want && err == nil
	if err != nil {
		_, _ = fmt.Fprintf(warn, "probe: warning: could not seize HID devices (%v); OS will receive reports in parallel\n", err)
	}
	return seizeStatus{requested: want, active: active, note: support.Note}
}

func listAll(infos []device.Info, identified []device.IdentifiedDevice) error {
	printRecognized(os.Stdout, identified)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "VID:PID\tBUS\tMANUFACTURER\tPRODUCT\tUSAGE\tIFACE\tPATH")
	for _, i := range infos {
		_, _ = fmt.Fprintf(w, "%04x:%04x\t%s\t%s\t%s\t%04x:%04x\t%d\t%s\n",
			i.VendorID, i.ProductID, i.BusType,
			truncate(i.Manufacturer, 24), truncate(i.Product, 32),
			i.UsagePage, i.Usage, i.Interface, i.Path,
		)
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("probe: flush: %w", err)
	}
	fmt.Fprintf(os.Stderr, "\n%d device(s) enumerated.\n", len(infos))
	return nil
}

func streamFirstRecognized(identified []device.IdentifiedDevice, emitEvents bool, usageSelector, pathSelector string, seize seizeStatus) error {
	target, ok := firstRecognized(identified)
	if !ok {
		return errors.New("probe: no recognized device connected (run probe without -stream to list everything)")
	}

	if emitEvents && usageSelector == "" && pathSelector == "" {
		return streamAllInputs(target, seize)
	}
	return streamSingleInterface(target, emitEvents, usageSelector, pathSelector, seize)
}

func streamSingleInterface(target device.IdentifiedDevice, emitEvents bool, usageSelector, pathSelector string, seize seizeStatus) error {
	iface, err := selectInterface(target, usageSelector, pathSelector)
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}

	stream, err := device.NewOpener().Open(iface.Path)
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}
	defer func() { _ = stream.Close() }()

	_, _ = fmt.Fprintf(os.Stdout, "Streaming %s (%04x:%04x) iface %d [usage %04x:%04x]\n",
		target.Known.Name, target.VendorID, target.ProductID,
		iface.Interface, iface.UsagePage, iface.Usage,
	)
	_, _ = fmt.Fprintln(os.Stdout, seize)
	_, _ = fmt.Fprintln(os.Stdout, "Press Ctrl-C to stop.")
	_, _ = fmt.Fprintln(os.Stdout)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	callback := newHexDumpCallback(time.Now())
	if emitEvents {
		callback = newEventsCallback(events.DeviceIDFor(target.VendorID, target.ProductID, iface.Serial))
	}

	err = stream.Read(ctx, callback)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		fmt.Fprintln(os.Stderr, "\nstopped.")
		return nil
	}
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}
	return nil
}

func streamAllInputs(target device.IdentifiedDevice, seize seizeStatus) error {
	inputs := target.Inputs()
	if len(inputs) == 0 {
		return fmt.Errorf("probe: %04x:%04x exposes no declared inputs", target.VendorID, target.ProductID)
	}

	_, _ = fmt.Fprintf(os.Stdout, "Streaming %s (%04x:%04x) — %d input(s):\n",
		target.Known.Name, target.VendorID, target.ProductID, len(inputs),
	)
	for _, in := range inputs {
		_, _ = fmt.Fprintf(os.Stdout, "  - %-8s iface %d [usage %04x:%04x] %s\n",
			in.Role, in.Info.Interface, in.Info.UsagePage, in.Info.Usage, in.Info.Path,
		)
	}
	_, _ = fmt.Fprintln(os.Stdout, seize)
	_, _ = fmt.Fprintln(os.Stdout, "Press Ctrl-C to stop.")
	_, _ = fmt.Fprintln(os.Stdout)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	deviceID := events.DeviceIDFor(target.VendorID, target.ProductID, inputs[0].Info.Serial)
	encoder := json.NewEncoder(os.Stdout)
	err := events.StreamAll(ctx, device.NewOpener(), deviceID, inputs, func(e protocol.InputEvent) error {
		return encoder.Encode(e)
	})
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		fmt.Fprintln(os.Stderr, "\nstopped.")
		return nil
	}
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}
	return nil
}

var effectStyles = map[string]vendor.Style{
	"off":      vendor.StyleOff,
	"static":   vendor.StyleStatic,
	"breath":   vendor.StyleBreath,
	"trigger":  vendor.StyleTrigger,
	"spectrum": vendor.StyleSpectrum,
	"user":     vendor.StyleUserLight,
}

// vendorLighting is the lighting change requested on the command line:
// an effect, optionally with one LED color.
type vendorLighting struct {
	effect *vendor.Effect
	led    *int
	color  *vendor.RGB
}

// vendorRequest is everything vendor mode was asked to do, in the order
// runVendor does it: rewrite the layout, change the lighting, then dump
// the slots (so -vendor-slots shows the result of the other flags).
type vendorRequest struct {
	readSlots     bool
	provision     bool
	factoryLayout bool
	lighting      vendorLighting
}

func runVendor(identified []device.IdentifiedDevice, req vendorRequest) error {
	target, ok := firstRecognized(identified)
	if !ok {
		return errors.New("probe: no recognized device connected")
	}
	iface, ok := target.VendorInfo()
	if !ok {
		return fmt.Errorf("probe: %s exposes no vendor interface", target.Known.Name)
	}
	client, err := vendor.Open(iface.Path, vendor.Limits{Slots: target.Known.Vendor.Slots, LEDs: target.Known.Vendor.LEDs})
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}
	defer func() { _ = client.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if req.provision || req.factoryLayout {
		name, layout := "KeyForge", target.Known.Vendor.Layout
		if req.factoryLayout {
			name, layout = "factory", target.Known.Vendor.Factory
		}
		written, err := client.ApplySlots(ctx, layout)
		if err != nil {
			return fmt.Errorf("probe: %w", err)
		}
		fmt.Printf("%s layout applied (%d slot(s) written)\n", name, written)
	}
	lighting := req.lighting
	// Colors are stored apart from the effect, so the color goes first:
	// a rejected LED then leaves the current effect untouched.
	if lighting.led != nil {
		if err := client.SetKeyColor(ctx, *lighting.led, *lighting.color); err != nil {
			return fmt.Errorf("probe: %w", err)
		}
		c := lighting.color
		fmt.Printf("led %d set to %02x%02x%02x\n", *lighting.led, c.R, c.G, c.B)
	}
	if lighting.effect != nil {
		if err := client.SetEffect(ctx, *lighting.effect); err != nil {
			return fmt.Errorf("probe: %w", err)
		}
		fmt.Printf("effect set to style 0x%02x\n", byte(lighting.effect.Style))
	}
	if req.readSlots {
		slots, err := client.ReadSlots(ctx)
		if err != nil {
			return fmt.Errorf("probe: %w", err)
		}
		fmt.Printf("%s input slots (layer 0):\n", target.Known.Name)
		for i, slot := range slots {
			fmt.Printf("  %2d  %s\n", i, slot)
		}
	}
	return nil
}

func parseLEDColor(s string) (int, vendor.RGB, error) {
	ledPart, hexPart, ok := strings.Cut(s, ":")
	led, err := strconv.Atoi(ledPart)
	if !ok || err != nil {
		return 0, vendor.RGB{}, fmt.Errorf("invalid LED color %q (want LED:RRGGBB)", s)
	}
	b, err := hex.DecodeString(hexPart)
	if err != nil || len(b) != 3 {
		return 0, vendor.RGB{}, fmt.Errorf("invalid color %q (want RRGGBB hex)", hexPart)
	}
	return led, vendor.RGB{R: b[0], G: b[1], B: b[2]}, nil
}

// selectInterface picks the interface of target to stream from. With
// pathSel set it matches by exact platform path; with usageSel set it
// matches by UsagePage:Usage and returns the first hit; with neither
// it returns the first interface tagged with the keyboard role from
// the registry. Callers enforce mutual exclusion of the two selectors.
func selectInterface(target device.IdentifiedDevice, usageSel, pathSel string) (device.Info, error) {
	if pathSel != "" {
		for _, info := range target.Interfaces {
			if info.Path == pathSel {
				return info, nil
			}
		}
		return device.Info{}, fmt.Errorf("no interface with path %q for %04x:%04x", pathSel, target.VendorID, target.ProductID)
	}
	if usageSel != "" {
		page, usage, err := parseUsageSelector(usageSel)
		if err != nil {
			return device.Info{}, err
		}
		for _, info := range target.Interfaces {
			if info.UsagePage == page && info.Usage == usage {
				return info, nil
			}
		}
		return device.Info{}, fmt.Errorf("no interface with usage %04x:%04x for %04x:%04x", page, usage, target.VendorID, target.ProductID)
	}
	for _, mi := range target.Inputs() {
		if mi.Role == device.RoleKeyboard {
			return mi.Info, nil
		}
	}
	return device.Info{}, fmt.Errorf("no keyboard-role interface declared for %04x:%04x", target.VendorID, target.ProductID)
}

func parseUsageSelector(s string) (uint16, uint16, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return 0, 0, fmt.Errorf("invalid usage selector %q (want AAAA:BBBB hex)", s)
	}
	page, err := strconv.ParseUint(parts[0], 16, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid usage page %q: %w", parts[0], err)
	}
	usage, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid usage %q: %w", parts[1], err)
	}
	return uint16(page), uint16(usage), nil
}

func newHexDumpCallback(start time.Time) func([]byte) error {
	return func(report []byte) error {
		_, _ = fmt.Fprintf(os.Stdout, "[t=%7.3fs len=%2d] %s\n",
			time.Since(start).Seconds(), len(report), formatHex(report),
		)
		return nil
	}
}

func newEventsCallback(deviceID protocol.DeviceID) func([]byte) error {
	mapper := events.NewKeyboardMapper(deviceID)
	encoder := json.NewEncoder(os.Stdout)
	return func(report []byte) error {
		evs, err := mapper.Map(report)
		if err != nil {
			// Some interfaces with UsagePage=Keyboard expose non-boot
			// report layouts. Skip silently so the operator only sees
			// the events that actually decoded.
			if errors.Is(err, events.ErrShortReport) {
				return nil
			}
			fmt.Fprintln(os.Stderr, err)
			return nil
		}
		for _, e := range evs {
			if err := encoder.Encode(e); err != nil {
				return fmt.Errorf("probe: encode event: %w", err)
			}
		}
		return nil
	}
}

func firstRecognized(devices []device.IdentifiedDevice) (device.IdentifiedDevice, bool) {
	for _, d := range devices {
		if d.Recognized {
			return d, true
		}
	}
	return device.IdentifiedDevice{}, false
}

func formatHex(report []byte) string {
	parts := make([]string, len(report))
	for i, b := range report {
		parts[i] = hex.EncodeToString([]byte{b})
	}
	return strings.Join(parts, " ")
}

func printRecognized(out io.Writer, devices []device.IdentifiedDevice) {
	recognized := make([]device.IdentifiedDevice, 0, len(devices))
	for _, d := range devices {
		if d.Recognized {
			recognized = append(recognized, d)
		}
	}
	if len(recognized) == 0 {
		return
	}

	_, _ = fmt.Fprintln(out, "Recognized devices:")
	for _, d := range recognized {
		_, _ = fmt.Fprintf(out, "  - %s (%04x:%04x) — %d interface(s)\n",
			d.Known.Name, d.VendorID, d.ProductID, len(d.Interfaces),
		)
	}
	_, _ = fmt.Fprintln(out)
}

func truncate(s string, n int) string {
	if n <= 1 || len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

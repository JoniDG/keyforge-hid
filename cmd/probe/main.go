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

	infos, err := device.NewEnumerator().List(context.Background())
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}

	identified := device.NewIdentifier(device.DefaultRegistry()).Identify(infos)

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
	if !want {
		return seizeStatus{requested: false, active: false, note: support.Note}
	}
	if err := device.EnableSeize(); err != nil {
		_, _ = fmt.Fprintf(warn, "probe: warning: could not seize HID devices (%v); OS will receive reports in parallel\n", err)
		return seizeStatus{requested: true, active: false, note: support.Note}
	}
	return seizeStatus{requested: true, active: true, note: support.Note}
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

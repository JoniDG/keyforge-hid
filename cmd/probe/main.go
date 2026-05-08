// Command probe lists HID devices visible to the host. With -stream
// it opens the first recognized device's primary input interface and
// dumps incoming reports until interrupted.
//
// This is a development helper for KeyForge phase 1; not part of the
// production binaries.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/JoniDG/keyforge-hid/internal/device"
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
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("probe: %w", err)
	}

	infos, err := device.NewEnumerator().List(context.Background())
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}

	identified := device.NewIdentifier(device.DefaultRegistry()).Identify(infos)

	if *stream {
		return streamFirstRecognized(identified)
	}
	return listAll(infos, identified)
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

func streamFirstRecognized(identified []device.IdentifiedDevice) error {
	target, ok := firstRecognized(identified)
	if !ok {
		return errors.New("probe: no recognized device connected (run probe without -stream to list everything)")
	}
	primary, err := target.PrimaryInput()
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}

	stream, err := device.NewOpener().Open(primary.Path)
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}
	defer func() { _ = stream.Close() }()

	_, _ = fmt.Fprintf(os.Stdout, "Streaming %s (%04x:%04x) iface %d [usage %04x:%04x]\n",
		target.Known.Name, target.VendorID, target.ProductID,
		primary.Interface, primary.UsagePage, primary.Usage,
	)
	_, _ = fmt.Fprintln(os.Stdout, "Press Ctrl-C to stop.")
	_, _ = fmt.Fprintln(os.Stdout)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	start := time.Now()
	err = stream.Read(ctx, func(report []byte) error {
		_, _ = fmt.Fprintf(os.Stdout, "[t=%7.3fs len=%2d] %s\n",
			time.Since(start).Seconds(), len(report), formatHex(report),
		)
		return nil
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

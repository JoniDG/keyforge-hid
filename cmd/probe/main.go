// Command probe lists HID devices visible to the host.
//
// This is a development helper for KeyForge phase 1; not part of the
// production binaries.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/JoniDG/keyforge-hid/internal/device"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	infos, err := device.NewEnumerator().List(context.Background())
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}

	identified := device.NewIdentifier(device.DefaultRegistry()).Identify(infos)
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

// Command probe lists HID devices visible to the host.
//
// This is a development helper for KeyForge phase 1; not part of the
// production binaries.
package main

import (
	"context"
	"fmt"
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

func truncate(s string, n int) string {
	if n <= 1 || len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

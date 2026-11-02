//go:build windows

package device

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestHardware_OpenCloseCycles_ShouldNotCorruptTheHeap opens and closes
// the keypad's Consumer Control collection KEYFORGE_HW_CYCLES times, each
// time closing with a read pending: the case where hidapi's hid_close
// frees memory the kernel still writes into. Needs the keypad plugged in.
//
// KEYFORGE_HW_CYCLES_HIDAPI=1 cycles through hidapi instead, to reproduce
// the corruption (0xc0000374 kills the test process when it hits).
func TestHardware_OpenCloseCycles_ShouldNotCorruptTheHeap(t *testing.T) {
	cycles, err := strconv.Atoi(os.Getenv("KEYFORGE_HW_CYCLES"))
	if err != nil || cycles <= 0 {
		t.Skip("set KEYFORGE_HW_CYCLES=<n> with the keypad plugged in")
	}
	infos, err := NewEnumerator().List(context.Background())
	require.NoError(t, err)
	path := ""
	for _, info := range infos {
		if info.VendorID == SideKeyboardKeypad.VendorID && info.ProductID == SideKeyboardKeypad.ProductID &&
			info.UsagePage == 0x000c && info.Usage == 0x0001 {
			path = info.Path
		}
	}
	require.NotEmpty(t, path, "keypad Consumer Control collection not found")

	open := openCollection
	if os.Getenv("KEYFORGE_HW_CYCLES_HIDAPI") == "1" {
		open = func(path string) (InputStream, error) {
			dev, err := OpenPath(path)
			if err != nil {
				return nil, err
			}
			return newStream(dev), nil
		}
	}
	for i := range cycles {
		stream, err := open(path)
		require.NoError(t, err, "cycle %d", i)
		// Longer than one poll, so the read is left pending at Close.
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		readErr := stream.Read(ctx, func([]byte) error { return nil })
		cancel()
		closeErr := stream.Close()
		require.True(t, errors.Is(readErr, context.DeadlineExceeded), "cycle %d: %v", i, readErr)
		require.NoError(t, closeErr, "cycle %d", i)
	}
}

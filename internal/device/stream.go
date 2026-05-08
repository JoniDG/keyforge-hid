package device

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sstallion/go-hid"
)

// Default buffer size used when reading raw HID input reports. Most
// USB HID devices fit in 64 bytes; larger devices will see truncation
// at this boundary.
const defaultReportBuffer = 64

// Default timeout passed to ReadWithTimeout when polling the device.
// Bounds the worst-case latency between context cancellation and the
// Read loop noticing it.
const defaultPollTimeout = 100 * time.Millisecond

// InputStream represents an opened HID device that delivers input
// reports until cancelled or closed.
type InputStream interface {
	// Read invokes fn for each input report received from the device.
	// It blocks until ctx is cancelled or fn returns a non-nil error.
	// The byte slice passed to fn is only valid for the duration of
	// the call; consumers that need to retain it must copy.
	Read(ctx context.Context, fn func(report []byte) error) error
	// Close releases the underlying device. Callers must call Close
	// even when Read returned an error.
	Close() error
}

// hidDevice is the package-private surface of *hid.Device that the
// stream depends on, isolated for testability.
type hidDevice interface {
	ReadWithTimeout(p []byte, timeout time.Duration) (int, error)
	Close() error
}

type hidStream struct {
	dev          hidDevice
	pollTimeout  time.Duration
	bufferLength int
}

func newStream(dev hidDevice) *hidStream {
	return &hidStream{
		dev:          dev,
		pollTimeout:  defaultPollTimeout,
		bufferLength: defaultReportBuffer,
	}
}

func (s *hidStream) Read(ctx context.Context, fn func([]byte) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	buf := make([]byte, s.bufferLength)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := s.dev.ReadWithTimeout(buf, s.pollTimeout)
		if errors.Is(err, hid.ErrTimeout) {
			continue
		}
		if err != nil {
			return fmt.Errorf("device.Read: %w: %w", ErrRead, err)
		}
		if n == 0 {
			continue
		}
		if err := fn(buf[:n]); err != nil {
			return err
		}
	}
}

func (s *hidStream) Close() error {
	if err := s.dev.Close(); err != nil {
		return fmt.Errorf("device.Close: %w: %w", ErrClose, err)
	}
	return nil
}

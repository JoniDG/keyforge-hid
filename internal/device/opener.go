package device

import (
	"fmt"

	"github.com/sstallion/go-hid"
)

// Opener opens HID devices by their platform-specific path.
type Opener interface {
	// Open returns an InputStream backed by the device located at
	// the given platform-specific path (the same value reported in
	// Info.Path during enumeration).
	Open(path string) (InputStream, error)
}

// NewOpener returns an Opener backed by hidapi via sstallion/go-hid.
func NewOpener() Opener {
	return &hidOpener{
		openPath: func(path string) (hidDevice, error) {
			return hid.OpenPath(path)
		},
	}
}

type hidOpener struct {
	openPath func(path string) (hidDevice, error)
}

func (o *hidOpener) Open(path string) (InputStream, error) {
	dev, err := o.openPath(path)
	if err != nil {
		return nil, fmt.Errorf("device.Open %q: %w: %w", path, ErrOpen, err)
	}
	return newStream(dev), nil
}

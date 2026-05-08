package device

import (
	"context"
	"fmt"

	"github.com/sstallion/go-hid"
)

// Enumerator lists HID devices visible to the host.
type Enumerator interface {
	// List returns information about every connected HID device. The
	// returned slice is non-nil even when empty. ctx is honored before
	// the underlying enumeration call and during each yielded device.
	List(ctx context.Context) ([]Info, error)
}

// NewEnumerator returns an Enumerator backed by hidapi via sstallion/go-hid.
func NewEnumerator() Enumerator {
	return &hidEnumerator{
		init: hid.Init,
		exit: hid.Exit,
		enumerate: func(vid, pid uint16, fn func(*hid.DeviceInfo) error) error {
			return hid.Enumerate(vid, pid, hid.EnumFunc(fn))
		},
	}
}

type hidEnumerator struct {
	init      func() error
	exit      func() error
	enumerate func(vid, pid uint16, fn func(*hid.DeviceInfo) error) error
}

func (e *hidEnumerator) List(ctx context.Context) ([]Info, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := e.init(); err != nil {
		return nil, fmt.Errorf("device.List: %w: %w", ErrInit, err)
	}

	infos := make([]Info, 0)
	walkErr := e.enumerate(0, 0, func(d *hid.DeviceInfo) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		infos = append(infos, infoFromHID(d))
		return nil
	})

	exitErr := e.exit()

	if walkErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("device.List: %w: %w", ErrEnumerate, walkErr)
	}
	if exitErr != nil {
		return nil, fmt.Errorf("device.List: %w: %w", ErrCleanup, exitErr)
	}
	return infos, nil
}

func infoFromHID(d *hid.DeviceInfo) Info {
	return Info{
		Path:         d.Path,
		VendorID:     d.VendorID,
		ProductID:    d.ProductID,
		Release:      d.ReleaseNbr,
		Manufacturer: d.MfrStr,
		Product:      d.ProductStr,
		Serial:       d.SerialNbr,
		UsagePage:    d.UsagePage,
		Usage:        d.Usage,
		Interface:    d.InterfaceNbr,
		BusType:      busTypeFromHID(d.BusType),
	}
}

func busTypeFromHID(b hid.BusType) BusType {
	switch b {
	case hid.BusUSB:
		return BusUSB
	case hid.BusBluetooth:
		return BusBluetooth
	case hid.BusI2C:
		return BusI2C
	case hid.BusSPI:
		return BusSPI
	default:
		return BusUnknown
	}
}

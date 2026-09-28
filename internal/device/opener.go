package device

import (
	"fmt"

	"github.com/sstallion/go-hid"
)

// Opener opens HID devices by their platform-specific path.
type Opener interface {
	// Open returns an InputStream backed by the device located at
	// the given platform-specific path (the same value reported in
	// Info.Path during enumeration). The device is opened in the mode
	// last accepted by SetSeize.
	Open(path string) (InputStream, error)
}

// NewOpener returns an Opener backed by hidapi via sstallion/go-hid.
func NewOpener() Opener {
	return &hidOpener{
		mode: hidapiOpenMode(),
		openPath: func(path string) (hidDevice, error) {
			return hid.OpenPath(path)
		},
	}
}

// OpenPath opens the HID device at path through hidapi in the mode last
// accepted by SetSeize. Every hidapi open outside this package must go
// through it: a bare hid.OpenPath can race the enumerator's Init/Exit
// and reset the open mode for the opens made here.
func OpenPath(path string) (*hid.Device, error) {
	var dev *hid.Device
	err := hidapiOpenMode().run(func() error {
		var err error
		dev, err = hid.OpenPath(path)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("device.OpenPath %q: %w", path, err)
	}
	return dev, nil
}

// openMode applies the requested open mode before an open.
type openMode struct {
	init          func() error
	seize         func() bool
	applyOpenMode func(seize bool)
}

func hidapiOpenMode() openMode {
	return openMode{init: hid.Init, seize: seizeRequested.Load, applyOpenMode: applyOpenMode}
}

// run applies the mode after an explicit init and calls open under
// hidapiMu, so neither the implicit init inside hidapi's open nor a
// concurrent enumeration can reset the mode before the device opens.
// Errors wrap ErrInit or ErrOpen.
func (m openMode) run(open func() error) error {
	hidapiMu.Lock()
	defer hidapiMu.Unlock()

	if err := m.init(); err != nil {
		return fmt.Errorf("%w: %w", ErrInit, err)
	}
	m.applyOpenMode(m.seize())
	if err := open(); err != nil {
		return fmt.Errorf("%w: %w", ErrOpen, err)
	}
	return nil
}

type hidOpener struct {
	mode     openMode
	openPath func(path string) (hidDevice, error)
}

func (o *hidOpener) Open(path string) (InputStream, error) {
	var dev hidDevice
	err := o.mode.run(func() error {
		var err error
		dev, err = o.openPath(path)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("device.Open %q: %w", path, err)
	}
	return newStream(dev), nil
}

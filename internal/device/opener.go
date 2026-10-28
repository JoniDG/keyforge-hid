package device

import (
	"fmt"

	"github.com/sstallion/go-hid"
)

// Opener opens enumerated HID interfaces.
type Opener interface {
	// Open returns an InputStream backed by the interface info
	// describes. The device is opened in the mode last accepted by
	// SetSeize.
	Open(info Info) (InputStream, error)
}

// NewOpener returns an Opener backed by hidapi via sstallion/go-hid.
func NewOpener() Opener {
	return &hidOpener{
		mode: hidapiOpenMode(),
		openPath: func(path string) (hidDevice, error) {
			return hid.OpenPath(path)
		},
		openKeyboard: platformKeyboardOpener,
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
	// openKeyboard, when set, reads keyboard interfaces instead of
	// hidapi. Windows needs it: it opens keyboards exclusively, so
	// hidapi cannot read their reports.
	openKeyboard func(path string) (InputStream, error)
}

func (o *hidOpener) Open(info Info) (InputStream, error) {
	path := info.Path
	if o.openKeyboard != nil && info.UsagePage == usagePageGenericDesktop && info.Usage == usageKeyboard {
		stream, err := o.openKeyboard(path)
		if err != nil {
			return nil, fmt.Errorf("device.Open %q: %w: %w", path, ErrOpen, err)
		}
		return stream, nil
	}
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

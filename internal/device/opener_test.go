package device

import (
	"context"
	"errors"
	"testing"

	"github.com/sstallion/go-hid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDarwinHIDAPI models hidapi's darwin open-mode state: hid_init
// resets the flag to seize whenever it has to create the HID manager,
// and hid_exit drops the manager.
type fakeDarwinHIDAPI struct {
	initialized bool
	exclusive   bool
	openedWith  []bool
	calls       []string
}

func (f *fakeDarwinHIDAPI) Init() error {
	f.calls = append(f.calls, "init")
	if !f.initialized {
		f.initialized = true
		f.exclusive = true
	}
	return nil
}

func (f *fakeDarwinHIDAPI) Exit() error {
	f.calls = append(f.calls, "exit")
	f.initialized = false
	return nil
}

func (f *fakeDarwinHIDAPI) SetOpenExclusive(exclusive bool) {
	f.calls = append(f.calls, "apply")
	f.exclusive = exclusive
}

func (f *fakeDarwinHIDAPI) Enumerate(_, _ uint16, _ func(*hid.DeviceInfo) error) error {
	f.calls = append(f.calls, "enumerate")
	return nil
}

func (f *fakeDarwinHIDAPI) OpenPath(_ string) (hidDevice, error) {
	// hid_open_path runs hid_init itself before opening.
	_ = f.Init()
	f.calls = append(f.calls, "open")
	f.openedWith = append(f.openedWith, f.exclusive)
	return &fakeHIDDevice{}, nil
}

func newOpenerFromFake(f *fakeDarwinHIDAPI, seize bool) *hidOpener {
	return &hidOpener{
		init:          f.Init,
		seize:         func() bool { return seize },
		applyOpenMode: f.SetOpenExclusive,
		openPath:      f.OpenPath,
	}
}

func TestOpener_Open_WhenPathOpens_ShouldReturnUsableStream(t *testing.T) {
	t.Parallel()
	fakeDev := &fakeHIDDevice{}
	o := &hidOpener{
		init:          func() error { return nil },
		seize:         func() bool { return false },
		applyOpenMode: func(bool) {},
		openPath:      func(_ string) (hidDevice, error) { return fakeDev, nil },
	}

	stream, err := o.Open("/dev/hidraw0")

	require.NoError(t, err)
	require.NotNil(t, stream)
	assert.NoError(t, stream.Close())
	assert.Equal(t, 1, fakeDev.closeCalls)
}

func TestOpener_Open_WhenPathFails_ShouldWrapErrOpen(t *testing.T) {
	t.Parallel()
	cause := errors.New("hidapi: path not found")
	o := &hidOpener{
		init:          func() error { return nil },
		seize:         func() bool { return false },
		applyOpenMode: func(bool) {},
		openPath:      func(_ string) (hidDevice, error) { return nil, cause },
	}

	stream, err := o.Open("missing")

	require.Error(t, err)
	assert.Nil(t, stream)
	assert.ErrorIs(t, err, ErrOpen)
	assert.ErrorIs(t, err, cause)
}

func TestOpener_Open_WhenInitFails_ShouldWrapErrInitWithoutOpening(t *testing.T) {
	t.Parallel()
	cause := errors.New("hidapi: manager unavailable")
	applied, opened := false, false
	o := &hidOpener{
		init:          func() error { return cause },
		seize:         func() bool { return false },
		applyOpenMode: func(bool) { applied = true },
		openPath: func(_ string) (hidDevice, error) {
			opened = true
			return &fakeHIDDevice{}, nil
		},
	}

	stream, err := o.Open("/dev/hidraw0")

	require.Error(t, err)
	assert.Nil(t, stream)
	assert.ErrorIs(t, err, ErrInit)
	assert.ErrorIs(t, err, cause)
	assert.False(t, applied, "open mode must not be applied when init fails")
	assert.False(t, opened, "device must not be opened when init fails")
}

func TestOpener_Open_WhenSharedRequested_ShouldApplyModeAfterInitAndBeforeOpen(t *testing.T) {
	t.Parallel()
	f := &fakeDarwinHIDAPI{}
	o := newOpenerFromFake(f, false)

	_, err := o.Open("DevSrvsID:0001")

	require.NoError(t, err)
	assert.Equal(t, []string{"init", "apply", "init", "open"}, f.calls)
	assert.Equal(t, []bool{false}, f.openedWith)
}

func TestOpener_Open_WhenEnumeratedAfterSharedRequest_ShouldStillOpenShared(t *testing.T) {
	t.Parallel()
	f := &fakeDarwinHIDAPI{}
	e := &hidEnumerator{init: f.Init, exit: f.Exit, enumerate: f.Enumerate}
	o := newOpenerFromFake(f, false)

	// Same order as Source.Stream: enumerate (Init/Exit), then open.
	_, err := e.List(context.Background())
	require.NoError(t, err)
	_, err = o.Open("DevSrvsID:0001")
	require.NoError(t, err)

	assert.Equal(t, []bool{false}, f.openedWith, "an Init/Exit cycle before the open must not turn a shared open into a seize")
}

func TestOpener_Open_WhenSeizeRequested_ShouldOpenExclusive(t *testing.T) {
	t.Parallel()
	f := &fakeDarwinHIDAPI{}
	o := newOpenerFromFake(f, true)

	_, err := o.Open("DevSrvsID:0001")

	require.NoError(t, err)
	assert.Equal(t, []bool{true}, f.openedWith)
}

func TestNewOpener_ShouldReturnNonNilImpl(t *testing.T) {
	t.Parallel()
	assert.NotNil(t, NewOpener())
}

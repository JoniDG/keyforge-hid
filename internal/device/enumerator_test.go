package device

import (
	"context"
	"errors"
	"testing"

	"github.com/sstallion/go-hid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeProvider struct {
	initErr    error
	exitErr    error
	exitCalls  int
	enumYields []*hid.DeviceInfo
	enumErr    error
}

func (f *fakeProvider) Init() error { return f.initErr }
func (f *fakeProvider) Exit() error { f.exitCalls++; return f.exitErr }

func (f *fakeProvider) Enumerate(_, _ uint16, fn func(*hid.DeviceInfo) error) error {
	for _, d := range f.enumYields {
		if err := fn(d); err != nil {
			return err
		}
	}
	return f.enumErr
}

func newEnumeratorFromFake(f *fakeProvider) *hidEnumerator {
	return &hidEnumerator{
		init:      f.Init,
		exit:      f.Exit,
		enumerate: f.Enumerate,
	}
}

func TestEnumerator_List_WhenNoDevices_ShouldReturnEmptyAndCleanUp(t *testing.T) {
	t.Parallel()
	f := &fakeProvider{}
	e := newEnumeratorFromFake(f)

	infos, err := e.List(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, infos)
	assert.Empty(t, infos)
	assert.Equal(t, 1, f.exitCalls)
}

func TestEnumerator_List_WhenDevicesYielded_ShouldMapAllFields(t *testing.T) {
	t.Parallel()
	f := &fakeProvider{
		enumYields: []*hid.DeviceInfo{
			{
				Path:         "DevSrvsID:0001",
				VendorID:     0x6D82,
				ProductID:    0xDC83,
				ReleaseNbr:   0x0100,
				MfrStr:       "SDINNOVATION",
				ProductStr:   "SIDE-KEYBOARD",
				SerialNbr:    "ABC123",
				UsagePage:    0x0001,
				Usage:        0x0006,
				InterfaceNbr: 0,
				BusType:      hid.BusUSB,
			},
			{
				Path:         "/dev/hidraw9",
				VendorID:     0x1234,
				ProductID:    0x5678,
				MfrStr:       "Acme",
				ProductStr:   "Widget",
				UsagePage:    0xFF00,
				Usage:        0x0002,
				InterfaceNbr: 2,
				BusType:      hid.BusBluetooth,
			},
		},
	}
	e := newEnumeratorFromFake(f)

	infos, err := e.List(context.Background())

	require.NoError(t, err)
	require.Len(t, infos, 2)
	assert.Equal(t, Info{
		Path:         "DevSrvsID:0001",
		VendorID:     0x6D82,
		ProductID:    0xDC83,
		Release:      0x0100,
		Manufacturer: "SDINNOVATION",
		Product:      "SIDE-KEYBOARD",
		Serial:       "ABC123",
		UsagePage:    0x0001,
		Usage:        0x0006,
		Interface:    0,
		BusType:      BusUSB,
	}, infos[0])
	assert.Equal(t, BusBluetooth, infos[1].BusType)
	assert.Equal(t, uint16(0xFF00), infos[1].UsagePage)
}

func TestEnumerator_List_WhenInitFails_ShouldWrapErrInitAndSkipExit(t *testing.T) {
	t.Parallel()
	cause := errors.New("hidapi: dlopen failed")
	f := &fakeProvider{initErr: cause}
	e := newEnumeratorFromFake(f)

	infos, err := e.List(context.Background())

	require.Error(t, err)
	assert.Nil(t, infos)
	assert.ErrorIs(t, err, ErrInit)
	assert.ErrorIs(t, err, cause)
	assert.Equal(t, 0, f.exitCalls, "Exit must not be called if Init fails")
}

func TestEnumerator_List_WhenEnumerateFails_ShouldWrapErrEnumerateAndStillCallExit(t *testing.T) {
	t.Parallel()
	cause := errors.New("hidapi: enumerate boom")
	f := &fakeProvider{enumErr: cause}
	e := newEnumeratorFromFake(f)

	infos, err := e.List(context.Background())

	require.Error(t, err)
	assert.Nil(t, infos)
	assert.ErrorIs(t, err, ErrEnumerate)
	assert.ErrorIs(t, err, cause)
	assert.Equal(t, 1, f.exitCalls, "Exit must run even if Enumerate fails")
}

func TestEnumerator_List_WhenExitFails_ShouldWrapErrCleanup(t *testing.T) {
	t.Parallel()
	cause := errors.New("hidapi: exit boom")
	f := &fakeProvider{exitErr: cause}
	e := newEnumeratorFromFake(f)

	infos, err := e.List(context.Background())

	require.Error(t, err)
	assert.Nil(t, infos)
	assert.ErrorIs(t, err, ErrCleanup)
	assert.ErrorIs(t, err, cause)
}

func TestEnumerator_List_WhenEnumerateAndExitBothFail_ShouldPreferEnumerateErr(t *testing.T) {
	t.Parallel()
	enumCause := errors.New("enumerate boom")
	exitCause := errors.New("exit boom")
	f := &fakeProvider{enumErr: enumCause, exitErr: exitCause}
	e := newEnumeratorFromFake(f)

	_, err := e.List(context.Background())

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEnumerate)
	assert.ErrorIs(t, err, enumCause)
	assert.NotErrorIs(t, err, ErrCleanup, "primary error should be the enumerate failure")
}

func TestEnumerator_List_WhenContextAlreadyCancelled_ShouldReturnCtxErrWithoutCallingHid(t *testing.T) {
	t.Parallel()
	f := &fakeProvider{}
	e := newEnumeratorFromFake(f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := e.List(ctx)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, f.exitCalls, "no hidapi calls if ctx already done")
}

func TestEnumerator_List_WhenContextCancelledMidEnumeration_ShouldReturnCtxErr(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	yields := []*hid.DeviceInfo{
		{Path: "/dev/hidraw0", VendorID: 0x1, ProductID: 0x1},
		{Path: "/dev/hidraw1", VendorID: 0x2, ProductID: 0x2},
	}
	f := &fakeProvider{}
	e := &hidEnumerator{
		init: f.Init,
		exit: f.Exit,
		enumerate: func(_, _ uint16, fn func(*hid.DeviceInfo) error) error {
			for i, d := range yields {
				if i == 1 {
					cancel()
				}
				if err := fn(d); err != nil {
					return err
				}
			}
			return nil
		},
	}

	_, err := e.List(ctx)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, f.exitCalls, "Exit must run even when ctx cancels mid-walk")
}

func TestNewEnumerator_ShouldReturnNonNilImpl(t *testing.T) {
	t.Parallel()
	assert.NotNil(t, NewEnumerator())
}

func TestBusTypeFromHID_ShouldMapEveryValue(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   hid.BusType
		want BusType
	}{
		{"unknown", hid.BusUnknown, BusUnknown},
		{"usb", hid.BusUSB, BusUSB},
		{"bluetooth", hid.BusBluetooth, BusBluetooth},
		{"i2c", hid.BusI2C, BusI2C},
		{"spi", hid.BusSPI, BusSPI},
		{"out-of-range falls back to unknown", hid.BusType(99), BusUnknown},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, busTypeFromHID(tc.in))
		})
	}
}

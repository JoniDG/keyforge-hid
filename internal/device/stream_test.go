package device

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sstallion/go-hid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeHIDDevice struct {
	reads     [][]byte
	readIndex int
	readErr   error
	readErrAt int

	closeErr   error
	closeCalls int
}

func (f *fakeHIDDevice) ReadWithTimeout(p []byte, _ time.Duration) (int, error) {
	if f.readErr != nil && f.readIndex == f.readErrAt {
		return 0, f.readErr
	}
	if f.readIndex >= len(f.reads) {
		return 0, hid.ErrTimeout
	}
	report := f.reads[f.readIndex]
	f.readIndex++
	return copy(p, report), nil
}

func (f *fakeHIDDevice) Close() error {
	f.closeCalls++
	return f.closeErr
}

func newTestStream(dev hidDevice) *hidStream {
	s := newStream(dev)
	s.pollTimeout = time.Millisecond
	return s
}

func TestStream_Read_WhenContextAlreadyCancelled_ShouldReturnCtxErrWithoutReading(t *testing.T) {
	t.Parallel()
	f := &fakeHIDDevice{}
	s := newTestStream(f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.Read(ctx, func([]byte) error { return nil })

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, f.readIndex, "device must not be polled when ctx is already done")
}

func TestStream_Read_WhenReportsArrive_ShouldInvokeCallbackPerReport(t *testing.T) {
	t.Parallel()
	reports := [][]byte{
		{0x01, 0x02, 0x03},
		{0xAA, 0xBB},
	}
	f := &fakeHIDDevice{reads: reports}
	s := newTestStream(f)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := make([][]byte, 0, len(reports))
	err := s.Read(ctx, func(report []byte) error {
		copyBuf := make([]byte, len(report))
		copy(copyBuf, report)
		got = append(got, copyBuf)
		if len(got) == len(reports) {
			cancel()
		}
		return nil
	})

	assert.ErrorIs(t, err, context.Canceled)
	require.Len(t, got, len(reports))
	assert.Equal(t, reports[0], got[0])
	assert.Equal(t, reports[1], got[1])
}

func TestStream_Read_WhenCallbackReturnsError_ShouldPropagateAsIs(t *testing.T) {
	t.Parallel()
	stop := errors.New("consumer says stop")
	f := &fakeHIDDevice{reads: [][]byte{{0x01}}}
	s := newTestStream(f)

	err := s.Read(context.Background(), func([]byte) error { return stop })

	assert.Equal(t, stop, err)
}

func TestStream_Read_WhenDeviceReadFails_ShouldWrapErrRead(t *testing.T) {
	t.Parallel()
	cause := errors.New("hidapi: device gone")
	f := &fakeHIDDevice{readErr: cause}
	s := newTestStream(f)

	err := s.Read(context.Background(), func([]byte) error { return nil })

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRead)
	assert.ErrorIs(t, err, cause)
}

func TestStream_Read_WhenReadTimesOut_ShouldKeepPollingUntilCtxCancels(t *testing.T) {
	t.Parallel()
	f := &fakeHIDDevice{}
	s := newTestStream(f)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	calls := 0
	err := s.Read(ctx, func([]byte) error {
		calls++
		return nil
	})

	assert.ErrorIs(t, err, context.DeadlineExceeded, "ctx deadline should win over the timeout loop")
	assert.NotErrorIs(t, err, ErrRead, "hid.ErrTimeout must not bubble up as ErrRead")
	assert.Equal(t, 0, calls, "fn must not be invoked when the device only reports timeouts")
}

func TestStream_Close_WhenDeviceCloseSucceeds_ShouldReturnNil(t *testing.T) {
	t.Parallel()
	f := &fakeHIDDevice{}
	s := newTestStream(f)

	require.NoError(t, s.Close())
	assert.Equal(t, 1, f.closeCalls)
}

func TestStream_Close_WhenDeviceCloseFails_ShouldWrapErrClose(t *testing.T) {
	t.Parallel()
	cause := errors.New("hidapi: close boom")
	f := &fakeHIDDevice{closeErr: cause}
	s := newTestStream(f)

	err := s.Close()

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrClose)
	assert.ErrorIs(t, err, cause)
}

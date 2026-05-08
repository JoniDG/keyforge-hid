package device

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpener_Open_WhenPathOpens_ShouldReturnUsableStream(t *testing.T) {
	t.Parallel()
	fakeDev := &fakeHIDDevice{}
	o := &hidOpener{
		openPath: func(_ string) (hidDevice, error) { return fakeDev, nil },
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
		openPath: func(_ string) (hidDevice, error) { return nil, cause },
	}

	stream, err := o.Open("missing")

	require.Error(t, err)
	assert.Nil(t, stream)
	assert.ErrorIs(t, err, ErrOpen)
	assert.ErrorIs(t, err, cause)
}

func TestNewOpener_ShouldReturnNonNilImpl(t *testing.T) {
	t.Parallel()
	assert.NotNil(t, NewOpener())
}

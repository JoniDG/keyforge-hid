package device

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetSeize_OnSupportedPlatform_BothDirectionsShouldSucceed(t *testing.T) {
	t.Parallel()
	if !PlatformSeizeSupport().Supported {
		t.Skip("platform stub — covered by the unsupported test below")
	}

	require.NoError(t, SetSeize(true))
	require.NoError(t, SetSeize(false))
}

func TestSetSeize_OnStubPlatform_DisableShouldSucceedEnableShouldError(t *testing.T) {
	t.Parallel()
	if PlatformSeizeSupport().Supported {
		t.Skip("platform supports seize — covered by the dedicated test above")
	}

	assert.NoError(t, SetSeize(false), "shared mode is the platform default and must always succeed")
	assert.ErrorIs(t, SetSeize(true), ErrSeizeNotImplemented)
}

func TestSetSeize_ShouldBeIdempotent(t *testing.T) {
	t.Parallel()

	first := SetSeize(true)
	second := SetSeize(true)
	assert.Equal(t, first, second, "calling SetSeize(true) twice must yield the same result")

	third := SetSeize(false)
	fourth := SetSeize(false)
	assert.Equal(t, third, fourth, "calling SetSeize(false) twice must yield the same result")
}

func TestPlatformSeizeSupport_NoteShouldBeNonEmpty(t *testing.T) {
	t.Parallel()

	support := PlatformSeizeSupport()

	assert.NotEmpty(t, support.Note, "Note should describe the current platform's seize state")
}

func TestPlatformSeizeSupport_ShouldMatchCurrentGOOS(t *testing.T) {
	t.Parallel()

	support := PlatformSeizeSupport()

	switch runtime.GOOS {
	case "darwin":
		assert.True(t, support.Supported, "darwin must report supported=true")
		assert.Contains(t, support.Note, "darwin")
	case "linux":
		assert.False(t, support.Supported, "linux stub must report supported=false")
		assert.Contains(t, support.Note, "linux")
	case "windows":
		assert.False(t, support.Supported, "windows stub must report supported=false")
		assert.Contains(t, support.Note, "windows")
	default:
		assert.False(t, support.Supported, "unknown platform must report supported=false")
	}
}

func TestSetSeize_ShouldRecordAcceptedModeForOpeners(t *testing.T) {
	// Not parallel: reads the process-wide mode other tests toggle.
	t.Cleanup(func() { seizeRequested.Store(false) })

	require.NoError(t, SetSeize(false))
	assert.False(t, seizeRequested.Load())

	err := SetSeize(true)
	if PlatformSeizeSupport().Supported {
		require.NoError(t, err)
		assert.True(t, seizeRequested.Load())
	} else {
		require.ErrorIs(t, err, ErrSeizeNotImplemented)
		assert.False(t, seizeRequested.Load(), "a rejected seize request must leave the mode shared")
	}
}

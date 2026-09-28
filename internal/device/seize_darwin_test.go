//go:build darwin

package device

import (
	"testing"

	"github.com/sstallion/go-hid"
	"github.com/stretchr/testify/assert"
)

func TestApplyOpenMode_ShouldSetHIDAPIExclusiveFlag(t *testing.T) {
	t.Parallel()

	applyOpenMode(true)
	assert.True(t, hid.GetOpenExclusive())

	applyOpenMode(false)
	assert.False(t, hid.GetOpenExclusive())
}

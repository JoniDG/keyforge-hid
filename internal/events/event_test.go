package events

import (
	"regexp"
	"testing"

	"github.com/JoniDG/keyforge-protocol/go/protocol"
	"github.com/stretchr/testify/assert"
)

var deviceIDPattern = regexp.MustCompile(`^VID_[0-9A-Fa-f]{4}_PID_[0-9A-Fa-f]{4}(_.+)?$`)

func TestDeviceIDFor_WithoutSerial_ShouldOmitTrailingSegment(t *testing.T) {
	t.Parallel()

	got := DeviceIDFor(0x6D82, 0xDC83, "")

	assert.Equal(t, protocol.DeviceID("VID_6D82_PID_DC83"), got)
	assert.Regexp(t, deviceIDPattern, string(got))
}

func TestDeviceIDFor_WithSerial_ShouldIncludeIt(t *testing.T) {
	t.Parallel()

	got := DeviceIDFor(0x6D82, 0xDC83, "ABC123")

	assert.Equal(t, protocol.DeviceID("VID_6D82_PID_DC83_ABC123"), got)
	assert.Regexp(t, deviceIDPattern, string(got))
}

func TestDeviceIDFor_ShouldUseUppercaseHex(t *testing.T) {
	t.Parallel()

	got := DeviceIDFor(0x00ab, 0x00cd, "")

	assert.Equal(t, protocol.DeviceID("VID_00AB_PID_00CD"), got)
}

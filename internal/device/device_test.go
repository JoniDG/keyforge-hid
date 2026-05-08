package device

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBusType_String_ShouldRenderHumanReadable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   BusType
		want string
	}{
		{BusUnknown, "Unknown"},
		{BusUSB, "USB"},
		{BusBluetooth, "Bluetooth"},
		{BusI2C, "I2C"},
		{BusSPI, "SPI"},
		{BusType(99), "Unknown"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.in.String())
		})
	}
}

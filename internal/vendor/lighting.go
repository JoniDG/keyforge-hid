package vendor

import "math"

// Style selects the keypad's lighting effect.
type Style byte

// Lighting styles understood by the firmware.
const (
	StyleOff       Style = 0x00
	StyleStatic    Style = 0x01
	StyleBreath    Style = 0x02
	StyleTrigger   Style = 0x03
	StyleSpectrum  Style = 0x04 // factory default (rainbow)
	StyleUserLight Style = 0x05 // per-key colors set with SetKeyColor
)

// HSV is a color as the firmware stores it for effects, each channel 0–255.
type HSV struct {
	H, S, V byte
}

// RGB is a per-key color.
type RGB struct {
	R, G, B byte
}

// Corrected reshapes c for LEDs whose brightness is linear in the channel
// value, unlike the gamma-encoded '#RRGGBB' a screen shows. Each channel
// becomes peak·(channel/peak)^gamma, where peak is the brightest channel:
// peak keeps its value, so the brightness picked is preserved, while the
// weaker channels drop so they stop washing the hue out toward white. A
// channel that was on never rounds down to off. Grays and pure primaries
// are unchanged, and gamma <= 0 returns c as is.
func (c RGB) Corrected(gamma float64) RGB {
	peak := max(c.R, c.G, c.B)
	if gamma <= 0 || peak == 0 {
		return c
	}
	channel := func(v byte) byte {
		if v == 0 {
			return 0
		}
		return byte(max(1, math.Round(float64(peak)*math.Pow(float64(v)/float64(peak), gamma))))
	}
	return RGB{R: channel(c.R), G: channel(c.G), B: channel(c.B)}
}

// Effect is the keypad-wide lighting configuration. The firmware
// always takes the full effect, so every field is sent on each write.
type Effect struct {
	Style Style
	// Speed is the animation speed; the vendor configurator uses 1–4.
	Speed byte
	// Mono renders the effect in Color only instead of cycling colors.
	Mono  bool
	Color HSV
}

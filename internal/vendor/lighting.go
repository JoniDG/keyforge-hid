package vendor

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

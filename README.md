# keyforge-hid

> HID device discovery and input event capture for [KeyForge](https://github.com/JoniDG/keyforge).

`keyforge-hid` is the hardware layer of KeyForge. It enumerates connected HID devices, identifies them by VID/PID, reads raw input reports, and emits typed `InputEvent` values for downstream consumers (typically `keyforge-core`).

## Status

🚧 **Pre-alpha.** Enumeration, VID/PID identification, input streaming and event mapping (keyboard + encoders) work. A public `hid` package now exposes them so other modules (e.g. `keyforge-core`) can consume decoded events without reaching into `internal/`.

## Build prerequisites

`keyforge-hid` uses [`sstallion/go-hid`](https://github.com/sstallion/go-hid), which wraps the `hidapi` C library. You need a C toolchain (cgo, enabled by default) and `hidapi` installed on your build host:

| Platform | Install |
|---|---|
| macOS (Homebrew) | `brew install hidapi` |
| Debian / Ubuntu | `sudo apt install libhidapi-dev libudev-dev` |
| Arch Linux | `sudo pacman -S hidapi` |
| Fedora | `sudo dnf install hidapi-devel systemd-devel` |
| Windows (MSYS2) | `pacman -S mingw-w64-x86_64-{gcc,hidapi}` |

On Linux, `libudev-dev` (or `systemd-devel`) is required because `sstallion/go-hid` vendors hidapi's hidraw backend, which depends on libudev for device enumeration.

On Windows the resulting binary needs `hidapi.dll` next to the `.exe` or in `PATH`.

## Quickstart

```bash
make probe
```

Prints a table of every HID device visible to the host:

```
VID:PID    BUS  MANUFACTURER  PRODUCT        USAGE      IFACE  PATH
6d82:dc83  USB  SDINNOVATION  SIDE-KEYBOARD  0001:0006  0      DevSrvsID:...
```

When the host has hardware that `keyforge-hid` recognizes (currently the SDINNOVATION SIDE-KEYBOARD reference keypad), a `Recognized devices:` summary is printed before the table with one line per identified device.

Plug the keypad and run it — the device should show up.

### Streaming raw input reports

```bash
make build && ./bin/probe -stream
```

`-stream` opens the first recognized device's primary keyboard interface and dumps every input report as hex until you Ctrl-C:

```
Streaming SDINNOVATION SIDE-KEYBOARD (6d82:dc83) iface 0 [usage 0001:0006]
Press Ctrl-C to stop.

[t=  0.123s len= 8] 00 00 04 00 00 00 00 00
[t=  0.156s len= 8] 00 00 00 00 00 00 00 00
```

### Streaming typed events

Add `-events` to decode each report into [`protocol.InputEvent`](https://github.com/JoniDG/keyforge-protocol/tree/main/go/protocol) JSON lines instead of hex bytes. One press of a key bound to `Ctrl+A` produces four events (one per state change):

```bash
make build && ./bin/probe -stream -events | jq
```

```json
{"action":"press","device_id":"VID_6D82_PID_DC83","input_id":"mod_lctrl","kind":"key","timestamp_ms":1746662400123}
{"action":"press","device_id":"VID_6D82_PID_DC83","input_id":"key_0x04","kind":"key","timestamp_ms":1746662400123}
{"action":"release","device_id":"VID_6D82_PID_DC83","input_id":"key_0x04","kind":"key","timestamp_ms":1746662400245}
{"action":"release","device_id":"VID_6D82_PID_DC83","input_id":"mod_lctrl","kind":"key","timestamp_ms":1746662400245}
```

`input_id` is derived from the HID byte: `mod_<name>` for the modifier byte (left/right ctrl/shift/alt/meta), `key_0x<hex>` for keycodes from the boot keyboard report. Encoder events come from the Consumer Control interface as `encoder_0` / `encoder_1` with `click`, `rotate_cw` or `rotate_ccw`.

#### Telling the keys and encoders apart

Out of the box the reference keypad sends `Ctrl+A` from every key and the same volume/mute events from both encoders. Provision it once so each input emits its own code (F13–F22 for the keys, distinct media usages for the second encoder). The change persists on the device and needs no `sudo`:

```bash
./bin/probe -provision -vendor-slots   # write the KeyForge layout and show it
./bin/probe -factory-layout            # undo it
```

See [the device doc](docs/hid-device-keyforge-keypad.md#21-keyforge-layout) for the full layout.

To light keys, paint them by input id (works while another process streams the keypad, no `sudo`):

```bash
./bin/probe -paint 'key_0x68=#ff0000,key_0x69=#00ff00'   # key 1 red, key 2 green
./bin/probe -vendor-effect spectrum                      # back to the factory rainbow
```

#### OS permissions

Reading raw HID reports from a keyboard-class interface can require elevated privileges, depending on the OS and on whether the device is seized. Without them, `-stream` fails with a permission error before the first read.

| Platform | What you need |
|---|---|
| **macOS** | Shared mode (`-shared`, or the library default `WithSeize(false)`) needs no `sudo`; grant **Input Monitoring** to your terminal if the open is rejected: *System Settings → Privacy & Security → Input Monitoring → +* and add `Terminal.app` (or iTerm, Warp, etc.), then restart the terminal session. Seizing a keyboard interface needs `sudo`. |
| **Linux** | Either run as `root` or add a udev rule that grants your user access to `/dev/hidraw*` for the device's VID/PID. The keypad's VID/PID is `0x6d82`/`0xdc83`. |
| **Windows** | Standard user permissions are usually enough for reading HID; opening with exclusive access (planned for a later phase) needs admin. |

Hijacking the keystrokes so the OS does not also receive them is a separate concern, tracked as a TBD in the project plan and resolved in a later phase.

## Using it as a library

The root `hid` package is the importable entry point. `Discover` reports the
recognized keypad (and the `DeviceID` its events will carry) before streaming;
`DiscoverDevice` returns the same device as a fully populated
`protocol.Device` (VID/PID, path, and the logical input catalog) so a consumer
can persist and describe it without opening it; `Provision` writes the KeyForge
input layout to the keypad so every key and encoder emits a distinct code (the
catalog describes the provisioned keypad; see
[the device doc](docs/hid-device-keyforge-keypad.md#21-keyforge-layout)),
and gives up with the wrapped `ctx.Err()` once its context is cancelled or
times out, so pass a short deadline if the keypad may not answer;
`PaintInputs` sets the LED color of the inputs the catalog flags `rgb`
(input id → `#RRGGBB`, `#000000` = off) and switches the keypad to the per-key
lighting effect, rejecting unknown or LED-less inputs with `ErrInputNotRGB`
before writing anything (colors are gamma-corrected for the device's LEDs so
they look closer to the screen; see
[the device doc](docs/hid-device-keyforge-keypad.md#color-response-measured));
`Stream` blocks delivering
decoded `protocol.InputEvent` values until the context is cancelled, the sink
returns an error, or a reader fails.

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/JoniDG/keyforge-hid"
	"github.com/JoniDG/keyforge-protocol/go/protocol"
)

func main() {
	src := hid.New() // hid.New(hid.WithSeize(true)) to take over the device (needs privileges)

	dev, err := src.Discover()
	if errors.Is(err, hid.ErrNoRecognizedDevice) {
		fmt.Println("plug in a recognized keypad")
		return
	} else if err != nil {
		panic(err)
	}
	fmt.Printf("streaming %s (%s)\n", dev.Name, dev.ID)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := src.Stream(ctx, func(e protocol.InputEvent) error {
		fmt.Printf("%s %s\n", e.Action, e.InputId)
		return nil
	}); err != nil {
		panic(err)
	}
}
```

Seizing the device (so the OS stops receiving its reports) is opt-in via
`WithSeize(true)` and needs the elevated privileges described under
[OS permissions](#os-permissions); the default keeps shared access.
On platforms where seizing is still a stub (Linux and Windows today),
`Stream` on a `WithSeize(true)` source fails with an error wrapping
`hid.ErrSeizeNotImplemented`, so callers can fall back to shared mode:

```go
err := hid.New(hid.WithSeize(true)).Stream(ctx, sink)
if errors.Is(err, hid.ErrSeizeNotImplemented) {
	err = hid.New().Stream(ctx, sink) // shared: the OS also receives the reports
}
```

`hid.SeizeSupport()` reports up front whether the current build can seize,
plus a one-line note for logs or UI.

## Layout

| Path | Purpose |
|---|---|
| `source.go` (package `hid`) | Public API: discover the recognized keypad and stream its decoded events |
| `cmd/probe/main.go` | CLI for manual device discovery and event inspection |
| `internal/device/` | Enumeration, identification, open/close lifecycle |
| `internal/events/` | Mapping from raw input reports to typed events |
| `internal/vendor/` | Driver for the keypad's vendor interface: input slots and RGB |

## Cross-platform support

The package targets Windows, macOS and Linux. OS-specific code lives in build-tagged files; the public API stays the same across platforms.

## License

[Apache 2.0](./LICENSE) — Copyright (c) 2026 Jonathan Daniel Gomez.

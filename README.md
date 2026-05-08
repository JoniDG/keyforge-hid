# keyforge-hid

> HID device discovery and input event capture for [KeyForge](https://github.com/JoniDG/keyforge).

`keyforge-hid` is the hardware layer of KeyForge. It enumerates connected HID devices, identifies them by VID/PID, reads raw input reports, and emits typed `InputEvent` values for downstream consumers (typically `keyforge-core`).

## Status

🚧 **Pre-alpha.** Phase 1.a in progress: HID enumeration works; input streaming, identification and event mapping are next.

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

Reports are still raw bytes at this stage; mapping them to typed `InputEvent`s is the next phase.

#### OS permissions

Reading raw HID reports from a keyboard-class interface requires elevated privileges on every major OS. Without them, `-stream` fails with a permission error before the first read.

| Platform | What you need |
|---|---|
| **macOS** | Grant **Input Monitoring** to your terminal: *System Settings → Privacy & Security → Input Monitoring → +* and add `Terminal.app` (or iTerm, Warp, etc.). Restart the terminal session. Child processes inherit the permission. |
| **Linux** | Either run as `root` or add a udev rule that grants your user access to `/dev/hidraw*` for the device's VID/PID. The keypad's VID/PID is `0x6d82`/`0xdc83`. |
| **Windows** | Standard user permissions are usually enough for reading HID; opening with exclusive access (planned for a later phase) needs admin. |

Hijacking the keystrokes so the OS does not also receive them is a separate concern, tracked as a TBD in the project plan and resolved in a later phase.

## Layout

| Path | Purpose |
|---|---|
| `cmd/probe/main.go` | CLI for manual device discovery and event inspection |
| `internal/device/` | Enumeration, identification, open/close lifecycle |
| `internal/events/` | Mapping from raw input reports to typed events |

## Cross-platform support

The package targets Windows, macOS and Linux. OS-specific code lives in build-tagged files; the public API stays the same across platforms.

## License

[Apache 2.0](./LICENSE) — Copyright (c) 2026 Jonathan Daniel Gomez.

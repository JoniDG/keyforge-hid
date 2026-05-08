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
| Debian / Ubuntu | `sudo apt install libhidapi-dev` |
| Arch Linux | `sudo pacman -S hidapi` |
| Fedora | `sudo dnf install hidapi-devel` |
| Windows (MSYS2) | `pacman -S mingw-w64-x86_64-{gcc,hidapi}` |

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

Plug the keypad and run it — the device should show up. Future phases will add input streaming and event mapping.

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

# keyforge-hid

> HID device discovery and input event capture for [KeyForge](https://github.com/JoniDG/keyforge).

`keyforge-hid` is the hardware layer of KeyForge. It enumerates connected HID devices, identifies them by VID/PID, reads raw input reports, and emits typed `InputEvent` values for downstream consumers (typically `keyforge-core`).

## Status

🚧 **Pre-alpha.** Scaffold only — no working code yet.

## Quickstart

```bash
go run ./cmd/probe
```

The `probe` binary lists HID devices visible to the host, then prints input events from a target device until you Ctrl-C.

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

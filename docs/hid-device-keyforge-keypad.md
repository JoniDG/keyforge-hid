# Reference device — SDINNOVATION "SIDE-KEYBOARD"

The KeyForge HID layer was bootstrapped against a no-name 10-key
mechanical keypad with two pressable rotary encoders, bought on Temu
under the brand **SDINNOVATION** and the product name **SIDE-KEYBOARD**.
The vendor ships proprietary Windows-only software to remap the keys
and drive the RGB; KeyForge exists in part to replace that.

This document captures everything we know about the device from
empirical inspection during Fase 1. Read it before adding a similar
device to the registry, or before debugging unexpected behavior with
this one specifically.

---

## 1. Identification

| Field                         | Value                            |
| ----------------------------- | -------------------------------- |
| Product Name                  | `SIDE-KEYBOARD`                  |
| Manufacturer / Vendor Name    | `SDINNOVATION`                   |
| Vendor ID (VID)               | `0x6D82` (28034 dec)             |
| Product ID (PID)              | `0xDC83` (56451 dec)             |
| Transport                     | USB                              |
| Top-Level Collections exposed | 8 (across 3 physical interfaces) |

Registered in `internal/device/known.go` as `SideKeyboardKeypad`.

### How the IDs were obtained (macOS)

```sh
# Lists every USB device with vendor/product info.
ioreg -p IOUSB -l | less

# Lists every HID interface (after plugging the keypad).
hidutil list

# Diff before/after plug to isolate the new entries:
hidutil list > before.txt && \
  # plug the keypad, wait a second \
  hidutil list > after.txt && \
  diff before.txt after.txt
```

The same approach works for any USB HID device when adding new
hardware to KeyForge.

---

## 2. Physical layout

- 10 mechanical keys, arranged 2×5.
- 2 rotary encoders, each clickable (presses register as a separate
  HID event from rotation).
- RGB backlight on every key. Currently undocumented — driven by the
  vendor's proprietary protocol over the vendor-specific HID interface
  (see §5.3); reverse-engineering is tracked under Fase 7.a.

Factory firmware mapping (out of the box, before any KeyForge
reconfiguration):

| Physical input             | Emitted event                  | HID detail                                  |
| -------------------------- | ------------------------------ | ------------------------------------------- |
| Any of the 10 keys         | `Ctrl+A` (Left Ctrl + key `A`) | Boot keyboard report `01 00 04 00 00 00 00 00` |
| Either encoder, rotate CW  | Consumer Volume Increment      | Consumer Control usage `0x00E9`             |
| Either encoder, rotate CCW | Consumer Volume Decrement      | Consumer Control usage `0x00EA`             |
| Either encoder, click      | Consumer Mute                  | Consumer Control usage `0x00E2`             |

The two physical encoders are **indistinguishable** through the
firmware's factory mapping: both emit identical Consumer Control
events. Differentiation requires reprogramming via the vendor protocol
(Fase 7.a).

---

## 3. HID topology

`hid.Enumerate` reports **8 entries** for this device. The count
fluctuates slightly between re-plugs because macOS occasionally
collapses or expands the entries for the multi-TLC interface, but the
underlying USB topology is stable.

Those 8 entries map to **3 physical USB interfaces**, identified by
their platform path (on macOS, a stable per-session `DevSrvsID:N`):

| Interface | Path (macOS, example)   | TLCs exposed                                                         | Role                                                             |
| --------- | ----------------------- | -------------------------------------------------------------------- | ---------------------------------------------------------------- |
| 0         | `DevSrvsID:4295184749`  | `0001:0006` (keyboard alt), `0001:0001/02/0c/80`, `000c:0001` (CC)   | Multi-TLC. KeyForge reads it for **encoder** Consumer Control.   |
| 1         | `DevSrvsID:4295184751`  | `0001:0006` (boot keyboard)                                          | KeyForge reads it for the **10 physical keys** (boot protocol).  |
| 2         | `DevSrvsID:4295184750`  | `0xFF00:0x0002` (vendor-specific)                                    | Vendor protocol — RGB + on-device config. Not opened by KeyForge today; target of Fase 7.a. |

Several observations that matter when writing code that opens this
device:

1. **The kernel's enumeration order is not stable across re-plugs.**
   Two interfaces share `UsagePage=0x0001 / Usage=0x0006` (interface 0
   has a 3-byte alt-keyboard TLC; interface 1 is the real boot
   keyboard). Matching by `(UsagePage, Usage)` alone could pick either
   one. `IdentifiedDevice.Inputs()` works around this by opening every
   declared interface and letting the per-role mappers reject reports
   they do not recognize.

2. **Interface 0 is multi-TLC**, so its six logical entries
   (`0001:0006`, `0001:0001`, `0001:0002`, `0001:000c`, `0001:0080`,
   `000c:0001`) all share the same platform path. Opening more than
   one of them via `IOHIDDeviceOpen` on macOS fails with
   "exclusive access and device already open". `events.openAll`
   collapses inputs by path and applies every registered mapper to the
   same stream.

3. **Interface 2 is the only place RGB and persistent firmware
   configuration live.** Touching it without a captured packet trace
   could brick the factory mapping; KeyForge does not open it.

---

## 4. Report layouts

### 4.1 Boot keyboard (interface 1)

Standard HID Boot Keyboard report — **8 bytes, no report ID prefix**.

| Byte    | Meaning                                                  |
| ------- | -------------------------------------------------------- |
| `[0]`   | Modifier bitmap (lctrl/lshift/lalt/lmeta/rctrl/rshift/ralt/rmeta) |
| `[1]`   | Reserved (always `0x00`)                                 |
| `[2..7]` | Up to 6 simultaneous keycodes (set semantics, not ordered) |

Example: pressing one of the 10 keys (factory-mapped to Ctrl+A):

```
01 00 04 00 00 00 00 00   # press   — lctrl modifier (bit 0) + keycode 0x04 (A)
00 00 00 00 00 00 00 00   # release — all zero
```

Decoded by `internal/events.KeyboardMapper`. The mapper is stateful: it
diffs each report against the previous one to emit `press` / `release`
events with stable input IDs (`mod_lctrl`, `key_0x04`, …).

### 4.2 Consumer Control (interface 0, TLC `000c:0001`)

Numbered reports — **3 bytes**, with the report ID in byte 0.

| Byte    | Meaning                                                   |
| ------- | --------------------------------------------------------- |
| `[0]`   | Report ID — always `0x03` for this device                 |
| `[1..2]` | 16-bit little-endian Consumer Control usage code         |

Usages observed on the reference keypad:

| Bytes (after report ID) | Usage  | HID Consumer Page meaning  | KeyForge action                       |
| ----------------------- | ------ | -------------------------- | ------------------------------------- |
| `E9 00`                 | `0x00E9` | Volume Increment           | `rotate_cw` on `encoder_0`            |
| `EA 00`                 | `0x00EA` | Volume Decrement           | `rotate_ccw` on `encoder_0`           |
| `E2 00`                 | `0x00E2` | Mute                       | `click` on `encoder_0`                |
| `00 00`                 | `0x0000` | release                    | ignored                               |

Every event arrives as a **press + release pair**; the release report
(`03 00 00`) is dropped silently because rotation has no release
semantic and the click is emitted on press.

Both physical encoders share the same `input_id` (`encoder_0`) because
the firmware emits indistinguishable reports. This is intentional and
will change once the vendor protocol is reversed and the encoders can
be reprogrammed to distinct codes (Fase 7.a).

Decoded by `internal/events.EncoderMapper`. The mapper is stateless
across reports.

### 4.3 Vendor-specific (interface 2, `FF00:0002`)

Not decoded today. Capturing the Chinese vendor application's USB
traffic with Wireshark + USBPcap is the planned starting point for
Fase 7.a, where we expect to find:

- RGB control commands (set color, set effect, brightness, …).
- Per-key remapping (which keycode each physical key emits).
- Encoder reassignment (which Consumer Control / keystroke each
  encoder emits).

Until then KeyForge leaves this interface alone.

---

## 5. Per-OS notes

### 5.1 macOS

Tested against macOS Sequoia (15.x).

- **Opening the device requires elevated permission**. `Input
  Monitoring` granted to the binary alone returns a clearer error code
  but still rejects the open. Running `cmd/probe` under `sudo` works
  for development; the productive flow (signed binary with
  Input Monitoring + Accessibility entitlements granted by the user
  through System Settings) is on the roadmap once the daemon ships in
  Fase 3.

- **Implicit seize on `IOHIDDeviceOpen`**. Opening any of this device's
  TLCs via hidapi appears to claim the OS routing for that TLC
  regardless of whether `kIOHIDOptionsTypeSeizeDevice` is passed. In
  practice this means the OS HID services stop delivering volume /
  mute / keyboard events to other apps as soon as KeyForge holds the
  TLC open. `device.SetSeize(true)` still calls
  `hid_darwin_set_open_exclusive(1)` so the intent is recorded and any
  future macOS release that distinguishes the option does the right
  thing — but it is not the observable gate today.

- **hidapi defaults to exclusive open**. Per
  `hid_darwin.c::hid_init`, the global option starts at
  `kIOHIDOptionsTypeSeizeDevice` ("Backward compatibility"). Callers
  that want shared mode must call `device.SetSeize(false)` explicitly;
  omitting the call leaves the device seized.

### 5.2 Linux (planned)

- `hid.OpenPath` will use the `hidraw` backend, which delivers a copy
  of every report to KeyForge — but the kernel's `evdev` subsystem
  also synthesizes keystrokes / consumer events for other userspace
  consumers in parallel. Blocking that channel requires
  `ioctl(EVIOCGRAB, 1)` on the evdev sibling node of the hidraw
  device. The sibling can be resolved by walking
  `/sys/class/hidraw/hidraw<N>/device/input/`.
- udev rules under `/etc/udev/rules.d/` will hand ownership of the
  device's hidraw and evdev nodes to a `keyforge` group so the daemon
  does not have to run as root.
- Implementation lives in `internal/device/seize_linux.go` (currently
  a stub).

### 5.3 Windows (planned)

- `hid.OpenPath` will use the user-mode HID class driver. The legacy
  keyboard stack (`WM_KEYDOWN`, system audio key dispatch) still
  delivers events to other apps in parallel; closing that channel
  needs `RegisterRawInputDevices` with `RIDEV_NOLEGACY | RIDEV_INPUTSINK`
  on the top-level usages the keypad exposes.
- Consumer Control (volume / mute) does not respect `RIDEV_NOLEGACY`
  fully; if the parallel delivery proves intolerable, the fallback is
  a signed kernel-mode HID filter driver — out of scope for the core
  project, deferred to packaging time.
- Implementation lives in `internal/device/seize_windows.go`
  (currently a stub).

---

## 6. Quickstart

```sh
# Build the development probe.
make build

# List every HID device visible to the host. Recognized devices appear
# under a "Recognized devices:" header.
./bin/probe

# Stream typed protocol.InputEvent JSON lines from every declared
# input on the recognized device (multi-interface, default). On macOS
# this needs sudo for the open() to succeed.
sudo ./bin/probe -stream -events

# Same, but skip the seize request (opens HID devices in shared mode).
# Useful when comparing OS-side behavior, even though on macOS the
# observable result is currently identical (see §5.1).
sudo ./bin/probe -stream -events -shared
```

### Debugging a specific interface

```sh
# Target one interface by HID usage page:usage (hex, no 0x prefix).
sudo ./bin/probe -stream -usage 000c:0001   # Consumer Control on iface 0
sudo ./bin/probe -stream -usage 0001:0006   # picks the first 0001:0006 match
                                            # (not deterministic — see §3)

# Or by exact platform path, when two interfaces share a usage.
sudo ./bin/probe -stream -path "DevSrvsID:4295184751"
```

Without `-events`, `-stream` produces a timestamped hex dump of every
incoming report — the right tool for inspecting an unfamiliar device
or layout.

---

## 7. Known limitations and open follow-ups

- **Encoder differentiation.** Both physical encoders share
  `input_id: encoder_0` because the factory firmware emits identical
  Consumer Control events. Fixing this needs the vendor protocol (Fase
  7.a) so the encoders can be reprogrammed to distinct usages or
  keystrokes.
- **RGB.** Not supported. Lives on the vendor-specific interface
  (`FF00:0002`), which KeyForge does not open today. Tracked under
  Fase 7.a together with encoder differentiation, because both depend
  on the same protocol capture.
- **`device_id` Serial stability.** `Info.Serial` is empty during
  `hid.Enumerate` but populated (e.g. `2A6E18426172`) once a stream
  starts on macOS. `events.DeviceIDFor` includes the serial when
  present, which means a binding keyed on `device_id` may not survive
  a re-plug if the kernel reassigns the serial. Verification across
  prolonged re-plugs is pending — it has to land before bindings go
  into persistent storage in Fase 3.
- **Cross-platform real seize.** `seize_linux.go` and
  `seize_windows.go` are stubs (see §5.2 and §5.3). They will be
  implemented when hardware is available on the corresponding
  platform for testing.

---

## 8. References

- Repo-local conventions and guard rails:
  [`../CLAUDE.md`](../CLAUDE.md).
- HID Consumer Control usage page reference: USB HID Usage Tables,
  Section 15 (Consumer Page, `0x0c`).
- hidapi: <https://github.com/libusb/hidapi>. Go bindings used here:
  <https://github.com/sstallion/go-hid>.

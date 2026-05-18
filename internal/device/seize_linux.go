//go:build linux

package device

// setSeize on Linux is a stub for the enable path. hidapi on Linux
// uses the hidraw backend, which delivers a copy of every report to
// our process — but the kernel's evdev subsystem still synthesizes
// keyboard/consumer events for other userspace consumers in parallel.
// Closing that second channel requires a separate evdev grab.
//
// Planned implementation when Linux hardware is available for testing:
//  1. Locate the evdev sibling of each opened hidraw device by walking
//     /sys/class/hidraw/<X>/device/input/inputN/eventM to resolve the
//     /dev/input/eventM node.
//  2. open(2) that node and ioctl(EVIOCGRAB, 1) it. The grab is
//     released on close or on EVIOCGRAB(0).
//  3. Ship udev rules under /etc/udev/rules.d/ that hand ownership of
//     the keypad's hidraw + evdev nodes to a "keyforge" group so the
//     daemon does not need root.
//
// Vendor-specific top-level collections (e.g. RGB) do not go through
// evdev, so no grab is needed for them.
//
// SetSeize(false) is a no-op: hidraw is already shared by default.
func setSeize(enabled bool) error {
	if !enabled {
		return nil
	}
	return ErrSeizeNotImplemented
}

func platformSeizeSupport() SeizeSupport {
	return SeizeSupport{
		Supported: false,
		Note:      "linux: not implemented (planned: EVIOCGRAB on the evdev sibling node + udev rules)",
	}
}

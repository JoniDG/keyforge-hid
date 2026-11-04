//go:build windows

package device

import "github.com/JoniDG/keyforge-hid/internal/vendor"

func platformLayout(v VendorInterface) []vendor.Slot { return v.WindowsLayout }

// platformReads is false for keyboard collections: Windows opens them
// exclusively, so KeyForge cannot read their reports, and WindowsLayout
// keeps every input off them.
func platformReads(role InputRole) bool { return role != RoleKeyboard }

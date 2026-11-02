//go:build !windows

package device

import "github.com/JoniDG/keyforge-hid/internal/vendor"

func platformLayout(v VendorInterface) []vendor.Slot { return v.Layout }

//go:build !windows

package device

// platformKeyboardOpener is nil outside Windows: hidapi reads keyboard
// interfaces like any other.
var platformKeyboardOpener func(path string) (InputStream, error)

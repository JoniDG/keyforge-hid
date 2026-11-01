//go:build !windows

package device

// platformCollectionOpener is nil outside Windows: hidapi reads every
// non-keyboard interface.
var platformCollectionOpener func(path string) (InputStream, error)

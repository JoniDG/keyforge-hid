package device

import (
	"errors"
	"fmt"
	"syscall"
)

// winErrorAccessDenied is Win32 ERROR_ACCESS_DENIED. It lives outside
// the windows build so markAccessDenied is tested on every platform.
const winErrorAccessDenied syscall.Errno = 5

// markAccessDenied tags a Win32 ERROR_ACCESS_DENIED with ErrAccessDenied.
// Windows opens mouse and keyboard collections exclusively, so reopening
// one fails the same way; callers need to tell that apart from an unplug.
func markAccessDenied(err error) error {
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == winErrorAccessDenied {
		return fmt.Errorf("%w: %w", ErrAccessDenied, err)
	}
	return err
}

// stripZeroReportID drops the 0x00 report ID Windows prepends to the
// reports of a collection that declares none, so reports look the same
// as on the other platforms (hidapi does the same).
func stripZeroReportID(report []byte) []byte {
	if len(report) > 0 && report[0] == 0 {
		return report[1:]
	}
	return report
}

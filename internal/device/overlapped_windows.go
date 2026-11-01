//go:build windows

package device

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

// platformCollectionOpener reads non-keyboard collections with
// overlapped I/O of its own instead of hidapi. hidapi's hid_close frees
// the read buffer and OVERLAPPED while a read is still pending (CancelIo
// only cancels the calling thread's I/O and does not wait), so the
// kernel can complete the read into freed memory: heap corruption
// (0xc0000374) after enough open/close cycles.
var platformCollectionOpener = openCollection

var (
	hidDLL = syscall.NewLazyDLL("hid.dll")

	procHidDGetPreparsedData  = hidDLL.NewProc("HidD_GetPreparsedData")
	procHidDFreePreparsedData = hidDLL.NewProc("HidD_FreePreparsedData")
	procHidPGetCaps           = hidDLL.NewProc("HidP_GetCaps")
	procCreateEventW          = kernel32.NewProc("CreateEventW")
	procGetOverlappedResult   = kernel32.NewProc("GetOverlappedResult")
)

const hidpStatusSuccess uint32 = 0x00110000

// hidpCaps mirrors HIDP_CAPS; only the input report length is read.
type hidpCaps struct {
	usage                   uint16
	usagePage               uint16
	inputReportByteLength   uint16
	outputReportByteLength  uint16
	featureReportByteLength uint16
	_                       [17 + 10]uint16
}

// overlappedStream reads one HID collection. buf and ol belong to the
// kernel while pending is set: Close waits for the read to finish before
// the handles go away.
type overlappedStream struct {
	handle      syscall.Handle
	ol          syscall.Overlapped
	buf         []byte
	pending     bool
	pollTimeout uint32
}

func openCollection(path string) (InputStream, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("device.openCollection: %w", err)
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return nil, fmt.Errorf("device.openCollection: CreateFile: %w", markAccessDenied(err))
	}
	length, err := inputReportLength(handle)
	if err != nil {
		_ = syscall.CloseHandle(handle)
		return nil, fmt.Errorf("device.openCollection: %w", err)
	}
	// Manual reset: the event stays signaled until the next read resets
	// it, so a completion between two waits is not lost.
	event, _, e := procCreateEventW.Call(0, 1, 0, 0)
	if event == 0 {
		_ = syscall.CloseHandle(handle)
		return nil, fmt.Errorf("device.openCollection: CreateEventW: %w", e)
	}
	s := &overlappedStream{
		handle:      handle,
		buf:         make([]byte, length),
		pollTimeout: uint32(defaultPollTimeout.Milliseconds()), // #nosec G115 -- 100 ms constant
	}
	s.ol.HEvent = syscall.Handle(event)
	return s, nil
}

// inputReportLength is the size ReadFile needs: the collection's longest
// input report plus its report ID byte.
func inputReportLength(handle syscall.Handle) (int, error) {
	var pp uintptr
	// The result is a BOOLEAN: only AL is defined, the rest of the
	// register is garbage.
	if r, _, e := procHidDGetPreparsedData.Call(uintptr(handle), uintptr(unsafe.Pointer(&pp))); byte(r) == 0 { // #nosec G103 G115 -- Win32 call; BOOLEAN is AL only
		return 0, fmt.Errorf("HidD_GetPreparsedData: %w", e)
	}
	defer func() { _, _, _ = procHidDFreePreparsedData.Call(pp) }()
	var caps hidpCaps
	if r, _, _ := procHidPGetCaps.Call(pp, uintptr(unsafe.Pointer(&caps))); uint32(r) != hidpStatusSuccess { // #nosec G103 G115 -- Win32 call; NTSTATUS is 32 bits
		return 0, fmt.Errorf("HidP_GetCaps: status %#x", uint32(r)) // #nosec G115 -- NTSTATUS is 32 bits
	}
	if caps.inputReportByteLength == 0 {
		return 0, errors.New("collection has no input reports")
	}
	return int(caps.inputReportByteLength), nil
}

func (s *overlappedStream) Read(ctx context.Context, fn func([]byte) error) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := s.poll()
		if err != nil {
			return fmt.Errorf("device.Read: %w: %w", ErrRead, err)
		}
		if n == 0 {
			continue
		}
		if err := fn(stripZeroReportID(s.buf[:n])); err != nil {
			return err
		}
	}
}

// poll starts a read unless one is pending and waits up to pollTimeout
// for it. It returns 0 bytes when the read is still pending.
func (s *overlappedStream) poll() (int, error) {
	if !s.pending {
		// ReadFile resets the event itself when the read starts.
		err := syscall.ReadFile(s.handle, s.buf, nil, &s.ol)
		if err != nil && !errors.Is(err, syscall.ERROR_IO_PENDING) {
			return 0, fmt.Errorf("ReadFile: %w", markAccessDenied(err))
		}
		s.pending = true
	}
	r, err := syscall.WaitForSingleObject(s.ol.HEvent, s.pollTimeout)
	switch r {
	case syscall.WAIT_TIMEOUT:
		return 0, nil
	case syscall.WAIT_OBJECT_0:
	default:
		return 0, fmt.Errorf("WaitForSingleObject: result %#x: %w", r, errors.Join(err))
	}
	s.pending = false
	n, err := s.result(false)
	if err != nil {
		return 0, fmt.Errorf("GetOverlappedResult: %w", markAccessDenied(err))
	}
	return n, nil
}

func (s *overlappedStream) result(wait bool) (int, error) {
	var n uint32
	var w uintptr
	if wait {
		w = 1
	}
	if r, _, e := procGetOverlappedResult.Call(uintptr(s.handle), uintptr(unsafe.Pointer(&s.ol)), uintptr(unsafe.Pointer(&n)), w); r == 0 { // #nosec G103 -- Win32 call
		return 0, e
	}
	return int(n), nil
}

// Close cancels a pending read and waits for the kernel to finish it
// before releasing the handles, so it never writes into a freed buffer.
func (s *overlappedStream) Close() error {
	if s.pending {
		// The result is ignored: ERROR_NOT_FOUND means the read already
		// completed, and if the cancel failed otherwise, the wait may
		// block until the next report. Waiting is still the only safe
		// option: freeing first is the corruption this reader avoids.
		_ = syscall.CancelIoEx(s.handle, &s.ol)
		_, _ = s.result(true)
		s.pending = false
	}
	if err := errors.Join(syscall.CloseHandle(s.handle), syscall.CloseHandle(s.ol.HEvent)); err != nil {
		return fmt.Errorf("device.Close: %w: %w", ErrClose, err)
	}
	return nil
}

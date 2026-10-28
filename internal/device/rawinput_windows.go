//go:build windows

package device

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

// platformKeyboardOpener reads keyboard interfaces through Raw Input:
// Windows opens keyboards exclusively, so hidapi cannot read them.
var platformKeyboardOpener = openRawKeyboard

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW        = user32.NewProc("RegisterClassExW")
	procCreateWindowExW         = user32.NewProc("CreateWindowExW")
	procDefWindowProcW          = user32.NewProc("DefWindowProcW")
	procGetMessageW             = user32.NewProc("GetMessageW")
	procDispatchMessageW        = user32.NewProc("DispatchMessageW")
	procRegisterRawInputDevices = user32.NewProc("RegisterRawInputDevices")
	procGetRawInputData         = user32.NewProc("GetRawInputData")
	procGetRawInputDeviceInfoW  = user32.NewProc("GetRawInputDeviceInfoW")
	procGetModuleHandleW        = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmInput         = 0x00FF
	ridInput        = 0x10000003
	ridiDeviceName  = 0x20000007
	rimTypeKeyboard = 1
	ridevInputSink  = 0x00000100
	riKeyBreak      = 0x0001
	// GetRawInputData and GetRawInputDeviceInfoW return a UINT, so their
	// -1 only fills the low 32 bits of the returned register.
	rawInputFailed = ^uint32(0)
)

type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   uintptr
	icon       uintptr
	cursor     uintptr
	background uintptr
	menuName   *uint16
	className  *uint16
	iconSm     uintptr
}

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	ptX     int32
	ptY     int32
	private uint32
}

type rawInputDevice struct {
	usagePage uint16
	usage     uint16
	flags     uint32
	target    uintptr
}

type rawInputHeader struct {
	kind   uint32
	size   uint32
	device uintptr
	wParam uintptr
}

type rawKeyboard struct {
	makeCode  uint16
	flags     uint16
	reserved  uint16
	vKey      uint16
	message   uint32
	extraInfo uint32
}

// rawHub owns the hidden window Raw Input delivers keyboard input to.
// One per process: a process holds a single registration per usage, so
// another keyboard registration in the same process (with another
// target window) would take the keys away from this one.
//
// The registration covers every keyboard and stays while the process
// runs, so the hub sees every key typed anywhere; handle drops all but
// F13–F24 before looking any further.
type rawHub struct {
	mu   sync.Mutex
	subs map[*rawKeyboardStream]struct{}
	// dead is set once the message loop has stopped; no stream can be
	// served after that.
	dead bool
	// names caches device names by Raw Input handle; only the hub
	// goroutine touches it.
	names map[uintptr]string
}

var (
	hubOnce sync.Once
	hub     *rawHub
	errHub  error
)

func openRawKeyboard(path string) (InputStream, error) {
	hubOnce.Do(func() { hub, errHub = startRawHub() })
	if errHub != nil {
		return nil, errHub
	}
	s := &rawKeyboardStream{hub: hub, path: path, reports: make(chan []byte, 64), done: make(chan struct{})}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.dead {
		return nil, errHubStopped
	}
	hub.subs[s] = struct{}{}
	return s, nil
}

var errHubStopped = errors.New("raw input message loop stopped")

func startRawHub() (*rawHub, error) {
	h := &rawHub{subs: map[*rawKeyboardStream]struct{}{}, names: map[uintptr]string{}}
	ready := make(chan error, 1)
	go h.run(ready)
	if err := <-ready; err != nil {
		return nil, fmt.Errorf("device.startRawHub: %w", err)
	}
	return h, nil
}

// run creates the window and pumps its messages for the life of the
// process. Window messages go to the thread that created the window, so
// the goroutine stays locked to it.
func (h *rawHub) run(ready chan<- error) {
	runtime.LockOSThread()
	hwnd, err := createInputWindow()
	if err != nil {
		ready <- err
		return
	}
	// INPUTSINK delivers keys while another app has the focus, which is
	// always the case for the daemon.
	dev := rawInputDevice{usagePage: usagePageGenericDesktop, usage: usageKeyboard, flags: ridevInputSink, target: hwnd}
	if r, _, e := procRegisterRawInputDevices.Call(uintptr(unsafe.Pointer(&dev)), 1, unsafe.Sizeof(dev)); r == 0 { // #nosec G103 -- Win32 call
		ready <- fmt.Errorf("RegisterRawInputDevices: %w", e)
		return
	}
	ready <- nil

	// Streams would otherwise wait forever on a loop that is gone.
	defer h.stop()

	var m winMsg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0) // #nosec G103 -- Win32 call
		if int32(r) <= 0 {                                                    // #nosec G115 -- GetMessage returns a BOOL: 0 (WM_QUIT) or -1 (error)
			return
		}
		if m.message == wmInput {
			h.handle(m.lParam)
		}
		// DefWindowProc frees the Raw Input data of WM_INPUT.
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m))) // #nosec G103 -- Win32 call
	}
}

// stop ends every open stream, whose Read then fails with ErrRead.
func (h *rawHub) stop() {
	h.mu.Lock()
	h.dead = true
	subs := h.subs
	h.subs = map[*rawKeyboardStream]struct{}{}
	h.mu.Unlock()
	for s := range subs {
		s.once.Do(func() { close(s.done) })
	}
}

// createInputWindow creates a window that is never shown: a regular
// top-level one rather than a message-only window, which INPUTSINK
// registrations are not documented to reach.
func createInputWindow() (uintptr, error) {
	instance, _, _ := procGetModuleHandleW.Call(0)
	className, err := syscall.UTF16PtrFromString("KeyForgeRawInput")
	if err != nil {
		return 0, fmt.Errorf("class name: %w", err)
	}
	wc := wndClassEx{wndProc: procDefWindowProcW.Addr(), instance: instance, className: className}
	wc.size = uint32(unsafe.Sizeof(wc))                                             // #nosec G103 G115 -- struct size fits uint32
	if r, _, e := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 { // #nosec G103 -- Win32 call
		return 0, fmt.Errorf("RegisterClassExW: %w", e)
	}
	hwnd, _, e := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, 0, 0, instance, 0) // #nosec G103 -- Win32 call
	if hwnd == 0 {
		return 0, fmt.Errorf("CreateWindowExW: %w", e)
	}
	return hwnd, nil
}

// handle decodes one WM_INPUT and hands F13–F24 transitions to the
// streams reading the interface they came from.
func (h *rawHub) handle(handle uintptr) {
	headerSize := unsafe.Sizeof(rawInputHeader{})
	var size uint32
	_, _, _ = procGetRawInputData.Call(handle, ridInput, 0, uintptr(unsafe.Pointer(&size)), headerSize) // #nosec G103 -- Win32 call
	if uintptr(size) < headerSize+unsafe.Sizeof(rawKeyboard{}) {
		return
	}
	// uint64 backing keeps the header's pointer-sized fields aligned.
	buf := make([]uint64, (size+7)/8)
	r, _, _ := procGetRawInputData.Call(handle, ridInput, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), headerSize) // #nosec G103 -- Win32 call
	if uint32(r) == rawInputFailed || r < headerSize+unsafe.Sizeof(rawKeyboard{}) {                                                     // #nosec G115 -- the UINT result is in the low 32 bits
		return
	}
	header := (*rawInputHeader)(unsafe.Pointer(&buf[0])) // #nosec G103 -- RAWINPUT layout
	if header.kind != rimTypeKeyboard {
		return
	}
	kb := (*rawKeyboard)(unsafe.Add(unsafe.Pointer(&buf[0]), headerSize)) // #nosec G103 -- RAWINPUT layout
	usage, ok := usageForVKey(kb.vKey)
	if !ok {
		return
	}
	name := h.deviceName(header.device)
	h.mu.Lock()
	var targets []*rawKeyboardStream
	for s := range h.subs {
		if sameInterface(s.path, name) {
			targets = append(targets, s)
		}
	}
	h.mu.Unlock()
	for _, s := range targets {
		s.deliver(usage, kb.flags&riKeyBreak == 0)
	}
}

func (h *rawHub) deviceName(device uintptr) string {
	if name, ok := h.names[device]; ok {
		return name
	}
	var chars uint32
	_, _, _ = procGetRawInputDeviceInfoW.Call(device, ridiDeviceName, 0, uintptr(unsafe.Pointer(&chars))) // #nosec G103 -- Win32 call
	if chars == 0 {
		return ""
	}
	buf := make([]uint16, chars)
	r, _, _ := procGetRawInputDeviceInfoW.Call(device, ridiDeviceName, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&chars))) // #nosec G103 -- Win32 call
	// Not cached on failure, so the next key from the device retries.
	if uint32(r) == rawInputFailed || uint32(r) == 0 { // #nosec G115 -- the UINT result is in the low 32 bits
		return ""
	}
	name := syscall.UTF16ToString(buf)
	h.names[device] = name
	return name
}

// rawKeyboardStream delivers the boot reports rebuilt from one keyboard
// interface's Raw Input.
type rawKeyboardStream struct {
	hub     *rawHub
	path    string
	held    heldKeys // only the hub goroutine touches it
	reports chan []byte
	done    chan struct{}
	once    sync.Once
}

var errStreamClosed = errors.New("stream closed")

// deliver blocks the message loop, and with it every stream, until the
// report is read or the stream is closed: dropping a report would lose a
// key transition. Whoever opens a stream must Close it as soon as Read
// returns (StreamAll does it with defer).
func (s *rawKeyboardStream) deliver(usage byte, down bool) {
	report, changed := s.held.update(usage, down)
	if !changed {
		return
	}
	select {
	case s.reports <- report:
	case <-s.done:
	}
}

func (s *rawKeyboardStream) Read(ctx context.Context, fn func([]byte) error) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.done:
			return fmt.Errorf("device.Read: %w: %w", ErrRead, errStreamClosed)
		case report := <-s.reports:
			if err := fn(report); err != nil {
				return err
			}
		}
	}
}

func (s *rawKeyboardStream) Close() error {
	s.once.Do(func() {
		s.hub.mu.Lock()
		delete(s.hub.subs, s)
		s.hub.mu.Unlock()
		close(s.done)
	})
	return nil
}

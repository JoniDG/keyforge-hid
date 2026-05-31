package hid

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/JoniDG/keyforge-hid/internal/device"
	"github.com/JoniDG/keyforge-protocol/go/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Reference keypad VID/PID and the DeviceID the pipeline derives from
// them. These lock the public contract keyforge-core depends on.
const (
	testVID      = 0x6D82
	testPID      = 0xDC83
	testDeviceID = protocol.DeviceID("VID_6D82_PID_DC83")
)

// fakeEnumerator returns a canned device list (or error) without touching
// hidapi, so the wrapper can be exercised on CI hosts with no hardware.
type fakeEnumerator struct {
	infos []device.Info
	err   error
}

func (e fakeEnumerator) List(context.Context) ([]device.Info, error) {
	return e.infos, e.err
}

// stubStream replays a fixed list of reports then blocks until ctx is
// cancelled. Safe under concurrent Close from another goroutine.
type stubStream struct {
	reports [][]byte
	closeMu sync.Mutex
	closed  chan struct{}
}

func newStubStream(reports ...[]byte) *stubStream {
	return &stubStream{reports: reports, closed: make(chan struct{})}
}

func (s *stubStream) Read(ctx context.Context, fn func([]byte) error) error {
	for _, r := range s.reports {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := fn(r); err != nil {
			return err
		}
	}
	<-ctx.Done()
	return ctx.Err()
}

func (s *stubStream) Close() error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	return nil
}

// errStream returns errReturn on the first Read call. Close is a no-op.
type errStream struct{ errReturn error }

func (s *errStream) Read(context.Context, func([]byte) error) error { return s.errReturn }
func (s *errStream) Close() error                                   { return nil }

// stubOpener returns preset streams by path, recording every Open call.
type stubOpener struct {
	streams map[string]device.InputStream
	errs    map[string]error
	mu      sync.Mutex
	calls   []string
}

func (o *stubOpener) Open(path string) (device.InputStream, error) {
	o.mu.Lock()
	o.calls = append(o.calls, path)
	o.mu.Unlock()
	if err, ok := o.errs[path]; ok {
		return nil, err
	}
	if s, ok := o.streams[path]; ok {
		return s, nil
	}
	return nil, errors.New("stubOpener: no stream for path")
}

func (o *stubOpener) openCalls() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.calls...)
}

// keyboardInfo / encoderInfo build enumeration entries whose VID/PID and
// usages match the SideKeyboardKeypad registry entry, so the real
// Identifier recognizes them and Inputs() tags them by role.
func keyboardInfo(path, serial string) device.Info {
	return device.Info{Path: path, VendorID: testVID, ProductID: testPID, Serial: serial, UsagePage: 0x0001, Usage: 0x0006}
}

func encoderInfo(path, serial string) device.Info {
	return device.Info{Path: path, VendorID: testVID, ProductID: testPID, Serial: serial, UsagePage: 0x000c, Usage: 0x0001}
}

func newSource(enum device.Enumerator, opener device.Opener, opts ...Option) *Source {
	s := &Source{
		enumerator: enum,
		identifier: device.NewIdentifier(device.DefaultRegistry()),
		opener:     opener,
		setSeize:   func(bool) error { return nil },
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func TestNew_ShouldWireRealPipelineWithSeizeDisabledByDefault(t *testing.T) {
	t.Parallel()
	s := New()
	require.NotNil(t, s.enumerator)
	require.NotNil(t, s.identifier)
	require.NotNil(t, s.opener)
	require.NotNil(t, s.setSeize)
	assert.False(t, s.seize)
}

func TestWithSeize_ShouldSetSeizeFlag(t *testing.T) {
	t.Parallel()
	assert.True(t, New(WithSeize(true)).seize)
	assert.False(t, New(WithSeize(false)).seize)
}

func TestDiscover_WhenDeviceRecognized_ShouldReturnIDAndName(t *testing.T) {
	t.Parallel()
	enum := fakeEnumerator{infos: []device.Info{keyboardInfo("kb", ""), encoderInfo("enc", "")}}
	s := newSource(enum, &stubOpener{})

	dev, err := s.Discover()

	require.NoError(t, err)
	assert.Equal(t, testDeviceID, dev.ID)
	assert.Equal(t, "SDINNOVATION SIDE-KEYBOARD", dev.Name)
}

func TestDiscover_WhenSerialPresent_ShouldAppendItToDeviceID(t *testing.T) {
	t.Parallel()
	enum := fakeEnumerator{infos: []device.Info{keyboardInfo("kb", "S1"), encoderInfo("enc", "S1")}}
	s := newSource(enum, &stubOpener{})

	dev, err := s.Discover()

	require.NoError(t, err)
	assert.Equal(t, protocol.DeviceID("VID_6D82_PID_DC83_S1"), dev.ID)
}

func TestDiscover_WhenNoRecognizedDevice_ShouldReturnSentinel(t *testing.T) {
	t.Parallel()
	enum := fakeEnumerator{infos: []device.Info{{Path: "x", VendorID: 0x1111, ProductID: 0x2222}}}
	s := newSource(enum, &stubOpener{})

	_, err := s.Discover()

	assert.ErrorIs(t, err, ErrNoRecognizedDevice)
}

func TestDiscover_WhenEnumerationFails_ShouldWrapError(t *testing.T) {
	t.Parallel()
	boom := errors.New("enumerate boom")
	s := newSource(fakeEnumerator{err: boom}, &stubOpener{})

	_, err := s.Discover()

	assert.ErrorIs(t, err, boom)
}

func TestDiscoverDevice_WhenDeviceRecognized_ShouldReturnFullProtocolDevice(t *testing.T) {
	t.Parallel()
	enum := fakeEnumerator{infos: []device.Info{keyboardInfo("kb", ""), encoderInfo("enc", "")}}
	s := newSource(enum, &stubOpener{})

	dev, err := s.DiscoverDevice()

	require.NoError(t, err)
	assert.Equal(t, testDeviceID, dev.Id)
	assert.Equal(t, "6d82", dev.VendorId)
	assert.Equal(t, "dc83", dev.ProductId)
	assert.Equal(t, "kb", dev.Path)
	assert.Equal(t, device.SideKeyboardKeypad.Controls, dev.Inputs)
}

func TestDiscoverDevice_WhenDescriptorStringsPresent_ShouldPopulateOptionalFields(t *testing.T) {
	t.Parallel()
	kb := keyboardInfo("kb", "S1")
	kb.Manufacturer = "SDINNOVATION"
	kb.Product = "SIDE-KEYBOARD"
	enum := fakeEnumerator{infos: []device.Info{kb, encoderInfo("enc", "S1")}}
	s := newSource(enum, &stubOpener{})

	dev, err := s.DiscoverDevice()

	require.NoError(t, err)
	assert.Equal(t, protocol.DeviceID("VID_6D82_PID_DC83_S1"), dev.Id)
	require.NotNil(t, dev.Manufacturer)
	assert.Equal(t, "SDINNOVATION", *dev.Manufacturer)
	require.NotNil(t, dev.Product)
	assert.Equal(t, "SIDE-KEYBOARD", *dev.Product)
	require.NotNil(t, dev.SerialNumber)
	assert.Equal(t, "S1", *dev.SerialNumber)
}

func TestDiscoverDevice_WhenDescriptorStringsEmpty_ShouldOmitOptionalFields(t *testing.T) {
	t.Parallel()
	enum := fakeEnumerator{infos: []device.Info{keyboardInfo("kb", ""), encoderInfo("enc", "")}}
	s := newSource(enum, &stubOpener{})

	dev, err := s.DiscoverDevice()

	require.NoError(t, err)
	assert.Nil(t, dev.Manufacturer)
	assert.Nil(t, dev.Product)
	assert.Nil(t, dev.SerialNumber)
}

func TestDiscoverDevice_ShouldReturnInputsClientsCannotMutateRegistry(t *testing.T) {
	t.Parallel()
	enum := fakeEnumerator{infos: []device.Info{keyboardInfo("kb", ""), encoderInfo("enc", "")}}
	s := newSource(enum, &stubOpener{})

	dev, err := s.DiscoverDevice()
	require.NoError(t, err)
	require.NotEmpty(t, dev.Inputs)

	dev.Inputs[0].Id = "mutated"
	assert.Equal(t, "key_0x04", device.SideKeyboardKeypad.Controls[0].Id)
}

func TestDiscoverDevice_WhenNoRecognizedDevice_ShouldReturnSentinel(t *testing.T) {
	t.Parallel()
	enum := fakeEnumerator{infos: []device.Info{{Path: "x", VendorID: 0x1111, ProductID: 0x2222}}}
	s := newSource(enum, &stubOpener{})

	_, err := s.DiscoverDevice()

	assert.ErrorIs(t, err, ErrNoRecognizedDevice)
}

func TestDiscoverDevice_WhenEnumerationFails_ShouldWrapError(t *testing.T) {
	t.Parallel()
	boom := errors.New("enumerate boom")
	s := newSource(fakeEnumerator{err: boom}, &stubOpener{})

	_, err := s.DiscoverDevice()

	assert.ErrorIs(t, err, boom)
}

func TestStream_WhenKeyboardReportArrives_ShouldDeliverEventTaggedWithDeviceID(t *testing.T) {
	t.Parallel()
	opener := &stubOpener{streams: map[string]device.InputStream{
		"kb":  newStubStream([]byte{0, 0, 0x04, 0, 0, 0, 0, 0}),
		"enc": newStubStream(),
	}}
	enum := fakeEnumerator{infos: []device.Info{keyboardInfo("kb", ""), encoderInfo("enc", "")}}
	s := newSource(enum, opener)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var got []protocol.InputEvent
	err := s.Stream(ctx, func(e protocol.InputEvent) error {
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
		cancel()
		return nil
	})

	require.NoError(t, err)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, got, 1)
	assert.Equal(t, protocol.InputActionPress, got[0].Action)
	assert.Equal(t, "key_0x04", got[0].InputId)
	assert.Equal(t, testDeviceID, got[0].DeviceId)
}

func TestStream_WhenContextDeadlineExceeded_ShouldReturnNil(t *testing.T) {
	t.Parallel()
	opener := &stubOpener{streams: map[string]device.InputStream{
		"kb":  newStubStream(),
		"enc": newStubStream(),
	}}
	enum := fakeEnumerator{infos: []device.Info{keyboardInfo("kb", ""), encoderInfo("enc", "")}}
	s := newSource(enum, opener)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := s.Stream(ctx, func(protocol.InputEvent) error { return nil })

	require.NoError(t, err)
}

func TestStream_WhenNoRecognizedDevice_ShouldReturnSentinelWithoutOpening(t *testing.T) {
	t.Parallel()
	opener := &stubOpener{}
	s := newSource(fakeEnumerator{}, opener)

	err := s.Stream(context.Background(), func(protocol.InputEvent) error { return nil })

	assert.ErrorIs(t, err, ErrNoRecognizedDevice)
	assert.Empty(t, opener.openCalls())
}

func TestStream_WhenEnumerationFails_ShouldWrapError(t *testing.T) {
	t.Parallel()
	boom := errors.New("enumerate boom")
	s := newSource(fakeEnumerator{err: boom}, &stubOpener{})

	err := s.Stream(context.Background(), func(protocol.InputEvent) error { return nil })

	assert.ErrorIs(t, err, boom)
}

func TestStream_WhenSeizeRequestedButUnsupported_ShouldFailBeforeOpening(t *testing.T) {
	t.Parallel()
	boom := errors.New("seize unsupported")
	opener := &stubOpener{}
	enum := fakeEnumerator{infos: []device.Info{keyboardInfo("kb", ""), encoderInfo("enc", "")}}
	s := newSource(enum, opener, WithSeize(true))
	s.setSeize = func(enabled bool) error {
		require.True(t, enabled)
		return boom
	}

	err := s.Stream(context.Background(), func(protocol.InputEvent) error { return nil })

	assert.ErrorIs(t, err, boom)
	assert.Empty(t, opener.openCalls())
}

func TestStream_WhenSinkReturnsError_ShouldPropagate(t *testing.T) {
	t.Parallel()
	opener := &stubOpener{streams: map[string]device.InputStream{
		"kb":  newStubStream([]byte{0, 0, 0x04, 0, 0, 0, 0, 0}),
		"enc": newStubStream(),
	}}
	enum := fakeEnumerator{infos: []device.Info{keyboardInfo("kb", ""), encoderInfo("enc", "")}}
	s := newSource(enum, opener)
	boom := errors.New("sink rejected")

	err := s.Stream(context.Background(), func(protocol.InputEvent) error { return boom })

	assert.ErrorIs(t, err, boom)
}

func TestStream_WhenReaderFails_ShouldCancelOthersAndWrapError(t *testing.T) {
	t.Parallel()
	boom := errors.New("read failed")
	// The reference keypad's two interfaces => two readers. The keyboard
	// reader fails immediately while the encoder reader blocks on ctx.
	// Stream must wrap and return the error instead of hanging on the
	// still-blocked reader.
	opener := &stubOpener{streams: map[string]device.InputStream{
		"kb":  &errStream{errReturn: boom},
		"enc": newStubStream(),
	}}
	enum := fakeEnumerator{infos: []device.Info{keyboardInfo("kb", ""), encoderInfo("enc", "")}}
	s := newSource(enum, opener)

	done := make(chan error, 1)
	go func() {
		done <- s.Stream(context.Background(), func(protocol.InputEvent) error { return nil })
	}()

	select {
	case err := <-done:
		assert.ErrorIs(t, err, boom)
	case <-time.After(2 * time.Second):
		t.Fatal("Stream deadlocked when a reader failed with siblings still blocked")
	}
}

func TestStream_WhenRecognizedDeviceHasNoDecodableInputs_ShouldReturnNilWithoutOpening(t *testing.T) {
	t.Parallel()
	// Recognized VID/PID but a usage that matches neither declared input,
	// so Inputs() is empty and StreamAll returns without opening anything.
	opener := &stubOpener{}
	enum := fakeEnumerator{infos: []device.Info{
		{Path: "x", VendorID: testVID, ProductID: testPID, Serial: "S1", UsagePage: 0xFF00, Usage: 0x0001},
	}}
	s := newSource(enum, opener)

	err := s.Stream(context.Background(), func(protocol.InputEvent) error {
		t.Fatal("sink must not be called")
		return nil
	})

	require.NoError(t, err)
	assert.Empty(t, opener.openCalls())
}

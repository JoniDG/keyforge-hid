package hid

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/JoniDG/keyforge-hid/internal/device"
	"github.com/JoniDG/keyforge-hid/internal/events"
	"github.com/JoniDG/keyforge-hid/internal/vendor"
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
	// honorCtx makes List fail with ctx.Err() once ctx is done, like the
	// real enumerator.
	honorCtx bool
}

func (e fakeEnumerator) List(ctx context.Context) ([]device.Info, error) {
	if e.honorCtx {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
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
		vendorSem:  make(chan struct{}, 1),
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
	require.NotNil(t, s.openVendor)
	assert.False(t, s.seize)
	assert.Equal(t, 1, cap(s.vendorSem))

	_, err := s.openVendor("keyforge-nonexistent-vendor-path", vendor.Limits{})
	assert.ErrorIs(t, err, vendor.ErrOpen)
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
	*dev.Inputs[0].Rgb = false
	dev.Inputs[0].Layout.X = 42
	want := device.SideKeyboardKeypad.Controls[0]
	assert.Equal(t, "key_0x68", want.Id)
	assert.True(t, *want.Rgb)
	assert.InDelta(t, 0, want.Layout.X, 0)
}

func TestCopyInputs_ShouldDeepCopyOptionalFields(t *testing.T) {
	t.Parallel()
	label, rgb, w, h := "Enter", true, 2.0, 0.5
	src := []protocol.Input{{
		Id: "k", Kind: protocol.InputKindKey, Label: &label, Rgb: &rgb,
		Layout: &protocol.InputLayout{X: 1, Y: 2, W: &w, H: &h},
	}}

	out := copyInputs(src)
	require.Equal(t, src, out)
	*out[0].Label = "x"
	*out[0].Rgb = false
	*out[0].Layout.W = 9
	*out[0].Layout.H = 9

	assert.Equal(t, "Enter", label)
	assert.True(t, rgb)
	assert.InDelta(t, 2.0, w, 0)
	assert.InDelta(t, 0.5, h, 0)
}

func TestCopyInputs_WhenOptionalFieldsAbsent_ShouldKeepThemNil(t *testing.T) {
	t.Parallel()
	out := copyInputs([]protocol.Input{{Id: "e", Kind: protocol.InputKindEncoder}})

	assert.Equal(t, []protocol.Input{{Id: "e", Kind: protocol.InputKindEncoder}}, out)
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
	opener := &stubOpener{}
	enum := fakeEnumerator{infos: []device.Info{keyboardInfo("kb", ""), encoderInfo("enc", "")}}
	s := newSource(enum, opener, WithSeize(true))
	s.setSeize = func(enabled bool) error {
		require.True(t, enabled)
		return device.ErrSeizeNotImplemented
	}

	err := s.Stream(context.Background(), func(protocol.InputEvent) error { return nil })

	assert.ErrorIs(t, err, ErrSeizeNotImplemented)
	assert.Empty(t, opener.openCalls())
}

func TestSeizeSupport_ShouldMirrorPlatformSeizeSupport(t *testing.T) {
	t.Parallel()
	want := device.PlatformSeizeSupport()

	supported, note := SeizeSupport()

	assert.Equal(t, want.Supported, supported)
	assert.Equal(t, want.Note, note)
	assert.NotEmpty(t, note)
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

func vendorInfo(path string) device.Info {
	return device.Info{Path: path, VendorID: testVID, ProductID: testPID, UsagePage: 0xFF00, Usage: 0x0002}
}

// fakeVendor records, in order, what the vendor client was asked to do.
type fakeVendor struct {
	applied    []vendor.Slot
	calls      []string
	applyErr   error
	colorErr   error
	effectErr  error
	closeErr   error
	closeCalls int
	// cancel, when set, simulates ctx being cancelled mid-session: it is
	// called on the first command, which then fails with ctx.Err() like
	// the real client.
	cancel context.CancelFunc
}

func (f *fakeVendor) cancelled(ctx context.Context) error {
	if f.cancel == nil {
		return nil
	}
	f.cancel()
	return ctx.Err()
}

func (f *fakeVendor) ApplySlots(ctx context.Context, want []vendor.Slot) (int, error) {
	f.applied = want
	if err := f.cancelled(ctx); err != nil {
		return 0, fmt.Errorf("vendor.ApplySlots: %w", err)
	}
	return len(want), f.applyErr
}

func (f *fakeVendor) SetKeyColor(ctx context.Context, led int, c vendor.RGB) error {
	f.calls = append(f.calls, fmt.Sprintf("color %d %02x%02x%02x", led, c.R, c.G, c.B))
	if err := f.cancelled(ctx); err != nil {
		return fmt.Errorf("vendor.SetKeyColor: %w", err)
	}
	return f.colorErr
}

func (f *fakeVendor) SetEffect(_ context.Context, e vendor.Effect) error {
	f.calls = append(f.calls, fmt.Sprintf("effect %02x", byte(e.Style)))
	return f.effectErr
}

func (f *fakeVendor) Close() error {
	f.closeCalls++
	return f.closeErr
}

// provisionSource builds a Source over the given enumeration whose vendor
// seam hands out client and records the path and limits it was opened
// with.
func provisionSource(infos []device.Info, client *fakeVendor, openErr error) (*Source, *string, *vendor.Limits) {
	s := newSource(fakeEnumerator{infos: infos}, &stubOpener{})
	var gotPath string
	var gotLimits vendor.Limits
	s.openVendor = func(path string, limits vendor.Limits) (vendorClient, error) {
		gotPath, gotLimits = path, limits
		if openErr != nil {
			return nil, openErr
		}
		return client, nil
	}
	return s, &gotPath, &gotLimits
}

func TestProvision_WhenVendorInterfaceEnumerated_ShouldApplyKeyForgeLayout(t *testing.T) {
	t.Parallel()
	applier := &fakeVendor{}
	s, path, limits := provisionSource([]device.Info{keyboardInfo("kb", ""), vendorInfo("vendor")}, applier, nil)

	err := s.Provision(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "vendor", *path)
	assert.Equal(t, vendor.Limits{Slots: 22, LEDs: 10}, *limits)
	assert.Equal(t, device.SideKeyboardKeypad.Vendor.Layout, applier.applied)
	assert.Equal(t, 1, applier.closeCalls)
}

func TestProvision_WhenVendorInterfaceNotEnumerated_ShouldReturnErrNoVendorInterface(t *testing.T) {
	t.Parallel()
	applier := &fakeVendor{}
	s, path, _ := provisionSource([]device.Info{keyboardInfo("kb", "")}, applier, nil)

	err := s.Provision(context.Background())

	assert.ErrorIs(t, err, ErrNoVendorInterface)
	assert.Empty(t, *path, "must not open anything")
}

func TestProvision_WhenNoRecognizedDevice_ShouldReturnSentinel(t *testing.T) {
	t.Parallel()
	s, _, _ := provisionSource([]device.Info{{Path: "x", VendorID: 0x1111, ProductID: 0x2222}}, &fakeVendor{}, nil)

	err := s.Provision(context.Background())

	assert.ErrorIs(t, err, ErrNoRecognizedDevice)
}

func TestProvision_WhenEnumerationFails_ShouldWrapError(t *testing.T) {
	t.Parallel()
	boom := errors.New("enumerate boom")
	s := newSource(fakeEnumerator{err: boom}, &stubOpener{})

	err := s.Provision(context.Background())

	assert.ErrorIs(t, err, boom)
}

func TestProvision_WhenOpenFails_ShouldWrapError(t *testing.T) {
	t.Parallel()
	boom := errors.New("open boom")
	s, _, _ := provisionSource([]device.Info{vendorInfo("vendor")}, &fakeVendor{}, boom)

	err := s.Provision(context.Background())

	assert.ErrorIs(t, err, boom)
}

func TestProvision_WhenApplyFails_ShouldWrapErrorAndStillClose(t *testing.T) {
	t.Parallel()
	boom := errors.New("apply boom")
	applier := &fakeVendor{applyErr: boom}
	s, _, _ := provisionSource([]device.Info{vendorInfo("vendor")}, applier, nil)

	err := s.Provision(context.Background())

	assert.ErrorIs(t, err, boom)
	assert.Equal(t, 1, applier.closeCalls)
}

func TestProvision_WhenCloseFails_ShouldWrapError(t *testing.T) {
	t.Parallel()
	boom := errors.New("close boom")
	s, _, _ := provisionSource([]device.Info{vendorInfo("vendor")}, &fakeVendor{closeErr: boom}, nil)

	err := s.Provision(context.Background())

	assert.ErrorIs(t, err, boom)
}

func TestProvision_WhenContextDoneBeforeEnumeration_ShouldReturnCtxErrWithoutOpening(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s, path, _ := provisionSource([]device.Info{vendorInfo("vendor")}, &fakeVendor{}, nil)
	s.enumerator = fakeEnumerator{infos: []device.Info{vendorInfo("vendor")}, honorCtx: true}

	err := s.Provision(ctx)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, *path, "must not open the vendor interface")
}

func TestProvision_WhenContextDoneBeforeOpening_ShouldReturnWrappedCtxErrWithoutOpening(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	s, path, _ := provisionSource([]device.Info{vendorInfo("vendor")}, &fakeVendor{}, nil)

	err := s.Provision(ctx)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Contains(t, err.Error(), "hid.Provision")
	assert.Empty(t, *path, "must not open the vendor interface")
}

func TestProvision_WhenContextCancelledDuringApply_ShouldWrapCtxErrAndStillClose(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	applier := &fakeVendor{cancel: cancel}
	s, _, _ := provisionSource([]device.Info{vendorInfo("vendor")}, applier, nil)

	err := s.Provision(ctx)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Contains(t, err.Error(), "hid.Provision")
	assert.Equal(t, 1, applier.closeCalls)
}

// The layout Provision writes, the catalog DiscoverDevice reports and the
// event mappers are three hand-written tables; this ties them together.
// Every slot the layout wires is fed through the real mapper as the
// report the keypad sends for it, and the input ids that come out must be
// exactly the catalog.
func TestSideKeyboardKeypad_WhenProvisioned_ShouldEmitExactlyTheCatalogInputs(t *testing.T) {
	t.Parallel()
	known := device.SideKeyboardKeypad

	emitted := map[string]bool{}
	for i, slot := range known.Vendor.Layout {
		var evs []protocol.InputEvent
		var err error
		switch slot.Type {
		case vendor.SlotDisabled:
			continue
		case vendor.SlotKeyboard:
			report := []byte{slot.Codes[0], 0x00, slot.Codes[1], 0, 0, 0, 0, 0}
			evs, err = events.NewKeyboardMapper(testDeviceID).Map(report)
		case vendor.SlotConsumer:
			evs, err = events.NewEncoderMapper(testDeviceID).Map([]byte{0x03, slot.Codes[0], slot.Codes[1]})
		default:
			t.Fatalf("slot %d: unexpected type %s", i, slot)
		}
		require.NoError(t, err, "slot %d", i)
		require.Len(t, evs, 1, "slot %d (%s) must emit exactly one event", i, slot)
		emitted[evs[0].InputId] = true
	}

	catalog := map[string]bool{}
	for _, in := range known.Controls {
		catalog[in.Id] = true
	}
	assert.Equal(t, catalog, emitted)
}

func TestDiscoverDevice_ShouldFlagOnlyKeysAsRGB(t *testing.T) {
	t.Parallel()
	s := newSource(fakeEnumerator{infos: []device.Info{keyboardInfo("kb", "")}}, &stubOpener{})

	dev, err := s.DiscoverDevice()

	require.NoError(t, err)
	for _, in := range dev.Inputs {
		if in.Kind == protocol.InputKindKey {
			require.NotNil(t, in.Rgb, in.Id)
			assert.True(t, *in.Rgb, in.Id)
		} else {
			assert.Nil(t, in.Rgb, in.Id)
		}
	}
}

func TestPaintInputs_WhenColorsValid_ShouldPaintCorrectedInLEDOrderThenSetUserLight(t *testing.T) {
	t.Parallel()
	client := &fakeVendor{}
	s, path, limits := provisionSource([]device.Info{keyboardInfo("kb", ""), vendorInfo("vendor")}, client, nil)

	err := s.PaintInputs(context.Background(), map[string]protocol.Color{
		"key_0x71": "#000000",
		"key_0x68": "#FF8000",
		"key_0x6a": "#00ff7f",
	})

	require.NoError(t, err)
	assert.Equal(t, "vendor", *path)
	assert.Equal(t, vendor.Limits{Slots: 22, LEDs: 10}, *limits)
	// Written gamma-corrected for the keypad's LEDs (vendor.RGB.Corrected).
	assert.Equal(t, []string{"color 0 ff1700", "color 2 00ff16", "color 9 000000", "effect 05"}, client.calls)
	assert.Equal(t, 1, client.closeCalls)
}

func TestPaintInputs_WhenColorsEmpty_ShouldOnlySetUserLight(t *testing.T) {
	t.Parallel()
	client := &fakeVendor{}
	s, _, _ := provisionSource([]device.Info{vendorInfo("vendor")}, client, nil)

	err := s.PaintInputs(context.Background(), nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"effect 05"}, client.calls)
}

func TestPaintInputs_WhenRequestInvalid_ShouldFailWithoutOpening(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		colors map[string]protocol.Color
		want   error
	}{
		{"unknown input", map[string]protocol.Color{"key_0x68": "#ff0000", "key_0x04": "#ff0000"}, ErrInputNotRGB},
		{"input without LED", map[string]protocol.Color{"encoder_0": "#ff0000"}, ErrInputNotRGB},
		{"missing hash", map[string]protocol.Color{"key_0x68": "ff0000"}, ErrInvalidColor},
		{"wrong length", map[string]protocol.Color{"key_0x68": "#fff"}, ErrInvalidColor},
		{"bad hex", map[string]protocol.Color{"key_0x68": "#gg0000"}, ErrInvalidColor},
		{"empty", map[string]protocol.Color{"key_0x68": ""}, ErrInvalidColor},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := &fakeVendor{}
			s, path, _ := provisionSource([]device.Info{vendorInfo("vendor")}, client, nil)

			err := s.PaintInputs(context.Background(), tc.colors)

			assert.ErrorIs(t, err, tc.want)
			assert.Contains(t, err.Error(), "hid.PaintInputs")
			assert.Empty(t, *path, "must not open the vendor interface")
			assert.Empty(t, client.calls)
		})
	}
}

func TestPaintInputs_WhenNoRecognizedDevice_ShouldReturnSentinel(t *testing.T) {
	t.Parallel()
	s, _, _ := provisionSource([]device.Info{{Path: "x", VendorID: 0x1111, ProductID: 0x2222}}, &fakeVendor{}, nil)

	err := s.PaintInputs(context.Background(), map[string]protocol.Color{"key_0x68": "#ff0000"})

	assert.ErrorIs(t, err, ErrNoRecognizedDevice)
}

func TestPaintInputs_WhenVendorInterfaceNotEnumerated_ShouldReturnErrNoVendorInterface(t *testing.T) {
	t.Parallel()
	s, path, _ := provisionSource([]device.Info{keyboardInfo("kb", "")}, &fakeVendor{}, nil)

	err := s.PaintInputs(context.Background(), map[string]protocol.Color{"key_0x68": "#ff0000"})

	assert.ErrorIs(t, err, ErrNoVendorInterface)
	assert.Empty(t, *path)
}

func TestPaintInputs_WhenOpenFails_ShouldWrapError(t *testing.T) {
	t.Parallel()
	s, _, _ := provisionSource([]device.Info{vendorInfo("vendor")}, &fakeVendor{}, fmt.Errorf("vendor.Open: %w", vendor.ErrOpen))

	err := s.PaintInputs(context.Background(), map[string]protocol.Color{"key_0x68": "#ff0000"})

	assert.ErrorIs(t, err, ErrVendorOpen)
	assert.Contains(t, err.Error(), "hid.PaintInputs")
}

func TestPaintInputs_WhenColorWriteFails_ShouldStopBeforeEffectAndStillClose(t *testing.T) {
	t.Parallel()
	client := &fakeVendor{colorErr: fmt.Errorf("vendor.SetKeyColor: %w", vendor.ErrNoAck)}
	s, _, _ := provisionSource([]device.Info{vendorInfo("vendor")}, client, nil)

	err := s.PaintInputs(context.Background(), map[string]protocol.Color{"key_0x68": "#ff0000", "key_0x69": "#00ff00"})

	assert.ErrorIs(t, err, ErrVendorNoAck)
	assert.Equal(t, []string{"color 0 ff0000"}, client.calls)
	assert.Equal(t, 1, client.closeCalls)
}

func TestPaintInputs_WhenEffectFails_ShouldWrapErrorAndStillClose(t *testing.T) {
	t.Parallel()
	client := &fakeVendor{effectErr: fmt.Errorf("vendor.SetEffect: %w", vendor.ErrWrite)}
	s, _, _ := provisionSource([]device.Info{vendorInfo("vendor")}, client, nil)

	err := s.PaintInputs(context.Background(), map[string]protocol.Color{"key_0x68": "#ff0000"})

	assert.ErrorIs(t, err, ErrVendorWrite)
	assert.Equal(t, 1, client.closeCalls)
}

func TestPaintInputs_WhenCloseFails_ShouldWrapError(t *testing.T) {
	t.Parallel()
	boom := errors.New("close boom")
	s, _, _ := provisionSource([]device.Info{vendorInfo("vendor")}, &fakeVendor{closeErr: boom}, nil)

	err := s.PaintInputs(context.Background(), map[string]protocol.Color{"key_0x68": "#ff0000"})

	assert.ErrorIs(t, err, boom)
}

func TestPaintInputs_WhenContextCancelledDuringPaint_ShouldWrapCtxErrAndStillClose(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeVendor{cancel: cancel}
	s, _, _ := provisionSource([]device.Info{vendorInfo("vendor")}, client, nil)

	err := s.PaintInputs(ctx, map[string]protocol.Color{"key_0x68": "#ff0000", "key_0x69": "#00ff00"})

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, []string{"color 0 ff0000"}, client.calls)
	assert.Equal(t, 1, client.closeCalls)
}

func TestVendorCalls_WhenAnotherIsRunning_ShouldWaitForIt(t *testing.T) {
	t.Parallel()
	s, path, _ := provisionSource([]device.Info{vendorInfo("vendor")}, &fakeVendor{}, nil)
	s.vendorSem <- struct{}{} // another vendor session in progress

	done := make(chan error, 1)
	go func() { done <- s.PaintInputs(context.Background(), map[string]protocol.Color{"key_0x68": "#ff0000"}) }()

	select {
	case err := <-done:
		t.Fatalf("PaintInputs returned while another vendor call held the interface: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	<-s.vendorSem
	require.NoError(t, <-done)
	assert.Equal(t, "vendor", *path)
}

func TestVendorCalls_WhenContextDoneWhileWaiting_ShouldReturnCtxErrWithoutOpening(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		call func(*Source, context.Context) error
		op   string
	}{
		{"Provision", (*Source).Provision, "hid.Provision"},
		{"PaintInputs", func(s *Source, ctx context.Context) error {
			return s.PaintInputs(ctx, map[string]protocol.Color{"key_0x68": "#ff0000"})
		}, "hid.PaintInputs"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, path, _ := provisionSource([]device.Info{vendorInfo("vendor")}, &fakeVendor{}, nil)
			s.vendorSem <- struct{}{}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()

			err := tc.call(s, ctx)

			assert.ErrorIs(t, err, context.DeadlineExceeded)
			assert.Contains(t, err.Error(), tc.op)
			assert.Empty(t, *path, "must not open the vendor interface")
		})
	}
}

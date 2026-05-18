package events

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JoniDG/keyforge-hid/internal/device"
	"github.com/JoniDG/keyforge-protocol/go/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubStream replays a fixed list of reports then blocks until ctx is
// cancelled. Safe under concurrent Close from another goroutine.
type stubStream struct {
	reports  [][]byte
	closeMu  sync.Mutex
	closed   chan struct{}
	closeErr error
}

func newStubStream(reports ...[]byte) *stubStream {
	return &stubStream{
		reports: reports,
		closed:  make(chan struct{}),
	}
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
	return s.closeErr
}

// errStream returns errReturn on the first Read call. Close is a no-op.
type errStream struct{ errReturn error }

func (s *errStream) Read(_ context.Context, _ func([]byte) error) error { return s.errReturn }
func (s *errStream) Close() error                                       { return nil }

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
	return nil, fmt.Errorf("stubOpener: no stream for %q", path)
}

// stubMapper allows pushing arbitrary mapper behavior into runReader
// for the rare branches StreamAll's real mappers can't exercise.
type stubMapper struct {
	out []protocol.InputEvent
	err error
}

func (m *stubMapper) Map(_ []byte) ([]protocol.InputEvent, error) {
	return m.out, m.err
}

func TestStreamAll_WhenInputsEmpty_ShouldReturnNilImmediately(t *testing.T) {
	t.Parallel()
	opener := &stubOpener{}

	err := StreamAll(context.Background(), opener, testDeviceID, nil, func(_ protocol.InputEvent) error {
		t.Fatal("sink must not be called")
		return nil
	})

	require.NoError(t, err)
	assert.Empty(t, opener.calls)
}

func TestStreamAll_WhenAllRolesUnknown_ShouldReturnNilWithoutOpening(t *testing.T) {
	t.Parallel()
	opener := &stubOpener{}
	inputs := []device.MatchedInput{{
		Info: device.Info{Path: "p1"},
		Role: device.InputRole("unrecognized"),
	}}

	err := StreamAll(context.Background(), opener, testDeviceID, inputs, func(_ protocol.InputEvent) error {
		t.Fatal("sink must not be called")
		return nil
	})

	require.NoError(t, err)
	assert.Empty(t, opener.calls)
}

func TestStreamAll_WhenOpenFails_ShouldReturnErrorAndNotInvokeSink(t *testing.T) {
	t.Parallel()
	boom := errors.New("can't open")
	opener := &stubOpener{errs: map[string]error{"p1": boom}}
	inputs := []device.MatchedInput{{
		Info: device.Info{Path: "p1"},
		Role: device.RoleKeyboard,
	}}

	err := StreamAll(context.Background(), opener, testDeviceID, inputs, func(_ protocol.InputEvent) error {
		t.Fatal("sink must not be called")
		return nil
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
}

func TestStreamAll_WhenSecondOpenFails_ShouldCloseFirstStreamBeforeReturning(t *testing.T) {
	t.Parallel()
	first := newStubStream()
	boom := errors.New("open p2 failed")
	opener := &stubOpener{
		streams: map[string]device.InputStream{"p1": first},
		errs:    map[string]error{"p2": boom},
	}
	inputs := []device.MatchedInput{
		{Info: device.Info{Path: "p1"}, Role: device.RoleKeyboard},
		{Info: device.Info{Path: "p2"}, Role: device.RoleEncoder},
	}

	err := StreamAll(context.Background(), opener, testDeviceID, inputs, func(_ protocol.InputEvent) error { return nil })

	require.ErrorIs(t, err, boom)
	select {
	case <-first.closed:
	default:
		t.Fatal("first stream was not closed when second open failed")
	}
}

func TestStreamAll_WhenKeyboardReportArrives_ShouldDeliverDecodedEventToSink(t *testing.T) {
	t.Parallel()
	opener := &stubOpener{
		streams: map[string]device.InputStream{
			"p1": newStubStream(
				[]byte{0, 0, 0x04, 0, 0, 0, 0, 0},
				[]byte{0, 0, 0, 0, 0, 0, 0, 0},
			),
		},
	}
	inputs := []device.MatchedInput{{Info: device.Info{Path: "p1"}, Role: device.RoleKeyboard}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var got []protocol.InputEvent
	err := StreamAll(ctx, opener, testDeviceID, inputs, func(e protocol.InputEvent) error {
		mu.Lock()
		got = append(got, e)
		done := len(got) == 2
		mu.Unlock()
		if done {
			cancel()
		}
		return nil
	})

	require.True(t, errors.Is(err, context.Canceled), "want context.Canceled, got %v", err)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, got, 2)
	assert.Equal(t, protocol.InputActionPress, got[0].Action)
	assert.Equal(t, "key_0x04", got[0].InputId)
	assert.Equal(t, protocol.InputActionRelease, got[1].Action)
}

func TestStreamAll_WhenEncoderReportArrives_ShouldDeliverDecodedEventToSink(t *testing.T) {
	t.Parallel()
	opener := &stubOpener{
		streams: map[string]device.InputStream{
			"p1": newStubStream(
				[]byte{0x03, 0xE9, 0x00},
				[]byte{0x03, 0x00, 0x00},
			),
		},
	}
	inputs := []device.MatchedInput{{Info: device.Info{Path: "p1"}, Role: device.RoleEncoder}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var got []protocol.InputEvent
	err := StreamAll(ctx, opener, testDeviceID, inputs, func(e protocol.InputEvent) error {
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
		cancel()
		return nil
	})

	require.True(t, errors.Is(err, context.Canceled))
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, got, 1)
	assert.Equal(t, protocol.InputActionRotateCw, got[0].Action)
	assert.Equal(t, protocol.InputKindEncoder, got[0].Kind)
	assert.Equal(t, "encoder_0", got[0].InputId)
}

func TestStreamAll_WithKeyboardAndEncoder_ShouldDeliverEventsFromBoth(t *testing.T) {
	t.Parallel()
	opener := &stubOpener{
		streams: map[string]device.InputStream{
			"kb":  newStubStream([]byte{0, 0, 0x04, 0, 0, 0, 0, 0}),
			"enc": newStubStream([]byte{0x03, 0xE9, 0x00}),
		},
	}
	inputs := []device.MatchedInput{
		{Info: device.Info{Path: "kb"}, Role: device.RoleKeyboard},
		{Info: device.Info{Path: "enc"}, Role: device.RoleEncoder},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	byKind := map[protocol.InputKind]int{}
	sink := func(e protocol.InputEvent) error {
		mu.Lock()
		byKind[e.Kind]++
		total := byKind[protocol.InputKindKey] + byKind[protocol.InputKindEncoder]
		mu.Unlock()
		if total >= 2 {
			cancel()
		}
		return nil
	}

	err := StreamAll(ctx, opener, testDeviceID, inputs, sink)

	require.True(t, errors.Is(err, context.Canceled))
	mu.Lock()
	defer mu.Unlock()
	assert.GreaterOrEqual(t, byKind[protocol.InputKindKey], 1)
	assert.GreaterOrEqual(t, byKind[protocol.InputKindEncoder], 1)
}

func TestStreamAll_WhenSinkReturnsError_ShouldCancelAndPropagate(t *testing.T) {
	t.Parallel()
	opener := &stubOpener{
		streams: map[string]device.InputStream{
			"p1": newStubStream([]byte{0, 0, 0x04, 0, 0, 0, 0, 0}),
		},
	}
	inputs := []device.MatchedInput{{Info: device.Info{Path: "p1"}, Role: device.RoleKeyboard}}
	boom := errors.New("sink rejected")

	err := StreamAll(context.Background(), opener, testDeviceID, inputs, func(_ protocol.InputEvent) error {
		return boom
	})

	assert.ErrorIs(t, err, boom)
}

func TestStreamAll_WhenContextCancelledExternally_ShouldReturnContextCanceled(t *testing.T) {
	t.Parallel()
	opener := &stubOpener{
		streams: map[string]device.InputStream{"p1": newStubStream()},
	}
	inputs := []device.MatchedInput{{Info: device.Info{Path: "p1"}, Role: device.RoleKeyboard}}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- StreamAll(ctx, opener, testDeviceID, inputs, func(_ protocol.InputEvent) error { return nil })
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("StreamAll did not return after external context cancellation")
	}
}

func TestStreamAll_WhenReaderReturnsFatalError_ShouldPropagate(t *testing.T) {
	t.Parallel()
	boom := errors.New("read failed")
	opener := &stubOpener{
		streams: map[string]device.InputStream{"p1": &errStream{errReturn: boom}},
	}
	inputs := []device.MatchedInput{{Info: device.Info{Path: "p1"}, Role: device.RoleKeyboard}}

	err := StreamAll(context.Background(), opener, testDeviceID, inputs, func(_ protocol.InputEvent) error { return nil })

	assert.ErrorIs(t, err, boom)
}

func TestStreamAll_WhenKeyboardEmitsShortReport_ShouldSkipSilentlyAndContinue(t *testing.T) {
	t.Parallel()
	opener := &stubOpener{
		streams: map[string]device.InputStream{
			"p1": newStubStream(
				[]byte{0, 0, 0},
				[]byte{0, 0, 0x04, 0, 0, 0, 0, 0},
			),
		},
	}
	inputs := []device.MatchedInput{{Info: device.Info{Path: "p1"}, Role: device.RoleKeyboard}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var got []protocol.InputEvent
	err := StreamAll(ctx, opener, testDeviceID, inputs, func(e protocol.InputEvent) error {
		got = append(got, e)
		cancel()
		return nil
	})

	require.True(t, errors.Is(err, context.Canceled))
	require.Len(t, got, 1)
	assert.Equal(t, "key_0x04", got[0].InputId)
}

func TestRunReader_WhenMapperReturnsNonShortError_ShouldEnqueueOnErrs(t *testing.T) {
	t.Parallel()
	stream := newStubStream([]byte{0, 0, 0x04, 0, 0, 0, 0, 0})
	boom := errors.New("decode failed")
	mapper := &stubMapper{err: boom}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan protocol.InputEvent, 1)
	errs := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)

	runReader(ctx, &wg, stream, []reportMapper{mapper}, events, errs)
	wg.Wait()

	select {
	case got := <-errs:
		assert.ErrorIs(t, got, boom)
	default:
		t.Fatal("runReader did not enqueue the fatal mapper error")
	}
}

func TestRunReader_WhenContextCancelledDuringSend_ShouldReturnWithoutEnqueueingError(t *testing.T) {
	t.Parallel()
	// Stream delivers one report so fn is called and blocks forever
	// trying to send into an unbuffered channel that nobody drains.
	// Cancelling the ctx makes the inner select take its Done case.
	stream := newStubStream([]byte{0, 0, 0x04, 0, 0, 0, 0, 0})
	mapper := &stubMapper{out: []protocol.InputEvent{{Action: protocol.InputActionPress, DeviceId: testDeviceID, InputId: "x", Kind: protocol.InputKindKey, TimestampMs: 1}}}

	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan protocol.InputEvent)
	errs := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)

	go runReader(ctx, &wg, stream, []reportMapper{mapper}, events, errs)

	time.Sleep(20 * time.Millisecond)
	cancel()
	wg.Wait()

	select {
	case got := <-errs:
		t.Fatalf("context cancellation must not be reported as a reader error: %v", got)
	default:
	}
}

func TestStreamAll_WhenTwoInputsShareSamePath_ShouldOpenOnceAndApplyBothMappers(t *testing.T) {
	t.Parallel()
	// Both inputs share the same platform path. The stream delivers a
	// 3-byte Consumer Control report: KeyboardMapper rejects it as
	// ErrShortReport (silent skip), EncoderMapper decodes it.
	opener := &stubOpener{
		streams: map[string]device.InputStream{
			"shared": newStubStream([]byte{0x03, 0xE9, 0x00}),
		},
	}
	inputs := []device.MatchedInput{
		{Info: device.Info{Path: "shared", UsagePage: 0x0001, Usage: 0x0006}, Role: device.RoleKeyboard},
		{Info: device.Info{Path: "shared", UsagePage: 0x000c, Usage: 0x0001}, Role: device.RoleEncoder},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var got []protocol.InputEvent
	err := StreamAll(ctx, opener, testDeviceID, inputs, func(e protocol.InputEvent) error {
		got = append(got, e)
		cancel()
		return nil
	})

	require.True(t, errors.Is(err, context.Canceled))
	require.Len(t, opener.calls, 1, "shared path must be opened exactly once")
	assert.Equal(t, "shared", opener.calls[0])
	require.Len(t, got, 1)
	assert.Equal(t, protocol.InputActionRotateCw, got[0].Action)
	assert.Equal(t, protocol.InputKindEncoder, got[0].Kind)
}

func TestStreamAll_WhenSharedPathDeliversReportsForBothMappers_ShouldEmitFromBoth(t *testing.T) {
	t.Parallel()
	// Same path, both an 8-byte boot keyboard report and a 3-byte
	// Consumer Control report. Each mapper picks up its own.
	opener := &stubOpener{
		streams: map[string]device.InputStream{
			"shared": newStubStream(
				[]byte{0, 0, 0x04, 0, 0, 0, 0, 0},
				[]byte{0x03, 0xE9, 0x00},
			),
		},
	}
	inputs := []device.MatchedInput{
		{Info: device.Info{Path: "shared"}, Role: device.RoleKeyboard},
		{Info: device.Info{Path: "shared"}, Role: device.RoleEncoder},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	byKind := map[protocol.InputKind]int{}
	err := StreamAll(ctx, opener, testDeviceID, inputs, func(e protocol.InputEvent) error {
		mu.Lock()
		byKind[e.Kind]++
		done := byKind[protocol.InputKindKey] >= 1 && byKind[protocol.InputKindEncoder] >= 1
		mu.Unlock()
		if done {
			cancel()
		}
		return nil
	})

	require.True(t, errors.Is(err, context.Canceled))
	require.Len(t, opener.calls, 1)
	mu.Lock()
	defer mu.Unlock()
	assert.GreaterOrEqual(t, byKind[protocol.InputKindKey], 1)
	assert.GreaterOrEqual(t, byKind[protocol.InputKindEncoder], 1)
}

func TestStreamAll_WhenSinkErrorsMidStream_ShouldStopCallingSinkButKeepDrainingEvents(t *testing.T) {
	t.Parallel()
	// Many press/release pairs so reader has plenty of follow-up
	// events to keep emitting after sink returns its error, exercising
	// the "drain but skip" branch of the main loop.
	reports := make([][]byte, 0, 100)
	for i := 0; i < 50; i++ {
		reports = append(reports, []byte{0, 0, 0x04, 0, 0, 0, 0, 0})
		reports = append(reports, []byte{0, 0, 0, 0, 0, 0, 0, 0})
	}
	opener := &stubOpener{
		streams: map[string]device.InputStream{"p1": newStubStream(reports...)},
	}
	inputs := []device.MatchedInput{{Info: device.Info{Path: "p1"}, Role: device.RoleKeyboard}}
	boom := errors.New("sink rejected")

	var callCount atomic.Int32
	err := StreamAll(context.Background(), opener, testDeviceID, inputs, func(_ protocol.InputEvent) error {
		callCount.Add(1)
		return boom
	})

	require.ErrorIs(t, err, boom)
	assert.Equal(t, int32(1), callCount.Load(), "sink must be invoked only once before subsequent events are skipped")
}

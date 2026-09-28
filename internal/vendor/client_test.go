package vendor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sstallion/go-hid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTransport records every write and answers each one with the
// replies produced by respond, mimicking the keypad's ack behavior.
type fakeTransport struct {
	writes     [][]byte
	pending    [][]byte
	respond    func(out []byte) [][]byte
	writeErr   error
	readErr    error
	onRead     func()
	closeErr   error
	closeCalls int
}

func (f *fakeTransport) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	f.writes = append(f.writes, append([]byte(nil), p...))
	if f.respond != nil {
		f.pending = append(f.pending, f.respond(p)...)
	}
	return len(p), nil
}

func (f *fakeTransport) ReadWithTimeout(p []byte, _ time.Duration) (int, error) {
	if f.onRead != nil {
		f.onRead()
	}
	if f.readErr != nil {
		return 0, f.readErr
	}
	if len(f.pending) == 0 {
		return 0, hid.ErrTimeout
	}
	next := f.pending[0]
	f.pending = f.pending[1:]
	return copy(p, next), nil
}

func (f *fakeTransport) Close() error {
	f.closeCalls++
	return f.closeErr
}

// deviceAck answers like the real keypad: 0xAA, the command byte (0x07
// for reads), then an echo of the rest of the payload. Slot reads carry
// slotData starting at byte 8, sliced by the requested offset.
func deviceAck(slotData []byte) func([]byte) [][]byte {
	return func(out []byte) [][]byte {
		pkt := out[1:]
		reply := make([]byte, reportLength)
		reply[0] = ackMarker
		if pkt[1] == cmdReadSlots {
			reply[1] = ackReadSlots
			copy(reply[2:8], pkt[2:8])
			off := int(pkt[3]) | int(pkt[4])<<8
			if off < len(slotData) {
				copy(reply[slotDataStart:], slotData[off:])
			}
			return [][]byte{reply}
		}
		reply[1] = pkt[1]
		reply[2] = 0x01
		copy(reply[3:], pkt[3:])
		return [][]byte{reply}
	}
}

var testLimits = Limits{Slots: 22, LEDs: 10}

func newTestClient(f *fakeTransport) *Client {
	return &Client{t: f, limits: testLimits, ackTimeout: 20 * time.Millisecond}
}

// packet returns the 64-byte report that followed the 0x00 report ID.
func packet(t *testing.T, raw []byte) []byte {
	t.Helper()
	require.Len(t, raw, 1+reportLength)
	require.Equal(t, byte(0x00), raw[0])
	return raw[1:]
}

func factorySlotData() []byte {
	data := make([]byte, 0, 28*slotSize)
	for range 10 {
		data = append(data, 0x20, 0x01, 0x04, 0x00)
	}
	for range 6 {
		data = append(data, 0x13, 0x00, 0x00, 0x00)
	}
	for range 2 {
		data = append(data, 0x30, 0xE2, 0x00, 0x00, 0x30, 0xE9, 0x00, 0x00, 0x30, 0xEA, 0x00, 0x00)
	}
	for range 6 {
		data = append(data, 0x13, 0x00, 0x00, 0x00)
	}
	return data
}

func TestOpen_WhenPathOpens_ShouldReturnClient(t *testing.T) {
	t.Parallel()
	f := &fakeTransport{}

	c, err := open("DevSrvsID:1", testLimits, func(string) (transport, error) { return f, nil })

	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, defaultAckTimeout, c.ackTimeout)
	assert.Equal(t, testLimits, c.limits)
	require.NoError(t, c.Close())
	assert.Equal(t, 1, f.closeCalls)
}

func TestOpen_WhenPathFails_ShouldWrapErrOpen(t *testing.T) {
	t.Parallel()
	cause := errors.New("hidapi: not found")

	c, err := open("missing", testLimits, func(string) (transport, error) { return nil, cause })

	assert.Nil(t, c)
	assert.ErrorIs(t, err, ErrOpen)
	assert.ErrorIs(t, err, cause)
}

func TestOpen_WhenOpenPathFails_ShouldForwardPathAndWrapErrOpen(t *testing.T) {
	t.Parallel()
	cause := errors.New("hidapi: path not found")
	var gotPath string

	c, err := Open("DevSrvsID:1", testLimits, func(p string) (*hid.Device, error) {
		gotPath = p
		return nil, cause
	})

	assert.Nil(t, c)
	assert.ErrorIs(t, err, ErrOpen)
	assert.ErrorIs(t, err, cause)
	assert.Equal(t, "DevSrvsID:1", gotPath)
}

func TestClient_Close_WhenTransportFails_ShouldWrapErrClose(t *testing.T) {
	t.Parallel()
	cause := errors.New("boom")
	c := newTestClient(&fakeTransport{closeErr: cause})

	err := c.Close()

	assert.ErrorIs(t, err, ErrClose)
	assert.ErrorIs(t, err, cause)
}

func TestClient_ReadSlots_WhenDeviceAnswers_ShouldDecodeEveryChunk(t *testing.T) {
	t.Parallel()
	f := &fakeTransport{respond: deviceAck(factorySlotData())}
	c := newTestClient(f)

	slots, err := c.ReadSlots(context.Background())

	require.NoError(t, err)
	require.Len(t, slots, 22)
	assert.Equal(t, KeyboardSlot(0x01, 0x04), slots[0])
	assert.Equal(t, KeyboardSlot(0x01, 0x04), slots[9])
	assert.Equal(t, DisabledSlot(), slots[10])
	assert.Equal(t, ConsumerSlot(0x00E2), slots[16])
	assert.Equal(t, ConsumerSlot(0x00E9), slots[20])
	assert.Equal(t, ConsumerSlot(0x00EA), slots[21])

	require.Len(t, f.writes, 2)
	assert.Equal(t, []byte{0x06, 0x08, 0x3A, 0x00, 0x00, 0x00, 0x00, 0x00}, packet(t, f.writes[0])[:8])
	assert.Equal(t, []byte{0x06, 0x08, 0x3A, 0x38, 0x00, 0x00, 0x00, 0x00}, packet(t, f.writes[1])[:8])
}

func TestClient_ReadSlots_WhenOnlyAStaleOffsetArrives_ShouldReturnErrNoAck(t *testing.T) {
	t.Parallel()
	f := &fakeTransport{respond: func(out []byte) [][]byte {
		reply := deviceAck(factorySlotData())(out)[0]
		reply[3] = 0x99
		return [][]byte{reply}
	}}
	c := newTestClient(f)

	_, err := c.ReadSlots(context.Background())

	assert.ErrorIs(t, err, ErrNoAck)
}

func TestClient_ReadSlots_WhenExchangeFails_ShouldWrapCause(t *testing.T) {
	t.Parallel()
	c := newTestClient(&fakeTransport{})

	_, err := c.ReadSlots(context.Background())

	assert.ErrorIs(t, err, ErrNoAck)
}

func TestClient_WriteSlot_WhenValid_ShouldSendSlotAtItsOffset(t *testing.T) {
	t.Parallel()
	f := &fakeTransport{respond: deviceAck(nil)}
	c := newTestClient(f)

	err := c.WriteSlot(context.Background(), 19, KeyboardSlot(0x00, 0x69))

	require.NoError(t, err)
	require.Len(t, f.writes, 1)
	assert.Equal(t,
		[]byte{0x06, 0x10, 0x07, 0x4C, 0x00, 0x00, 0x00, 0x00, 0x20, 0x00, 0x69, 0x00, 0x00},
		packet(t, f.writes[0])[:13])
}

func TestClient_WriteSlot_WhenIndexIsInvalid_ShouldReturnErrInvalidArgument(t *testing.T) {
	t.Parallel()
	for _, index := range []int{-1, 22, 1 << 62} {
		f := &fakeTransport{}
		c := newTestClient(f)

		err := c.WriteSlot(context.Background(), index, DisabledSlot())

		assert.ErrorIs(t, err, ErrInvalidArgument, "index %d", index)
		assert.Empty(t, f.writes)
	}
}

func TestClient_WriteSlot_WhenWriteFails_ShouldWrapErrWrite(t *testing.T) {
	t.Parallel()
	cause := errors.New("pipe")
	c := newTestClient(&fakeTransport{writeErr: cause})

	err := c.WriteSlot(context.Background(), 0, DisabledSlot())

	assert.ErrorIs(t, err, ErrWrite)
	assert.ErrorIs(t, err, cause)
}

func TestClient_SetEffect_ShouldEncodeStyleSpeedModeAndColor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		effect   Effect
		wantMode byte
	}{
		{name: "multicolor", effect: Effect{Style: StyleSpectrum, Speed: 2}, wantMode: 0x02},
		{name: "mono", effect: Effect{Style: StyleStatic, Speed: 3, Mono: true, Color: HSV{H: 0x10, S: 0xFF, V: 0x80}}, wantMode: 0x03},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &fakeTransport{respond: deviceAck(nil)}
			c := newTestClient(f)

			err := c.SetEffect(context.Background(), tt.effect)

			require.NoError(t, err)
			require.Len(t, f.writes, 1)
			e := tt.effect
			assert.Equal(t,
				[]byte{0x06, 0x0B, 0x0B, 0x00, 0x00, 0x01, 0x00, byte(e.Style), e.Speed, tt.wantMode, 0x01, 0x01, 0x00, e.Color.H, e.Color.S, e.Color.V, 0x00},
				packet(t, f.writes[0])[:17])
		})
	}
}

func TestClient_SetEffect_WhenNoAck_ShouldReturnErrNoAck(t *testing.T) {
	t.Parallel()
	c := newTestClient(&fakeTransport{})

	err := c.SetEffect(context.Background(), Effect{Style: StyleOff})

	assert.ErrorIs(t, err, ErrNoAck)
}

func TestClient_SetKeyColor_WhenValid_ShouldSendColorAtLedOffset(t *testing.T) {
	t.Parallel()
	f := &fakeTransport{respond: deviceAck(nil)}
	c := newTestClient(f)

	err := c.SetKeyColor(context.Background(), 9, RGB{R: 0xFF, G: 0x80, B: 0x01})

	require.NoError(t, err)
	require.Len(t, f.writes, 1)
	assert.Equal(t,
		[]byte{0x06, 0x14, 0x03, 0x1B, 0x00, 0x00, 0x00, 0x00, 0xFF, 0x80, 0x01, 0x00},
		packet(t, f.writes[0])[:12])
}

func TestClient_SetKeyColor_WhenLedIsInvalid_ShouldReturnErrInvalidArgument(t *testing.T) {
	t.Parallel()
	for _, led := range []int{-1, 10, 1 << 62} {
		f := &fakeTransport{}
		c := newTestClient(f)

		err := c.SetKeyColor(context.Background(), led, RGB{})

		assert.ErrorIs(t, err, ErrInvalidArgument, "led %d", led)
		assert.Empty(t, f.writes)
	}
}

func TestClient_SetKeyColor_WhenNoAck_ShouldReturnErrNoAck(t *testing.T) {
	t.Parallel()
	c := newTestClient(&fakeTransport{})

	err := c.SetKeyColor(context.Background(), 0, RGB{})

	assert.ErrorIs(t, err, ErrNoAck)
}

func TestClient_Exchange_WhenUnrelatedReportsArriveFirst_ShouldSkipThemUntilAck(t *testing.T) {
	t.Parallel()
	short := []byte{ackMarker, cmdWriteSlot}
	otherCmd := make([]byte, reportLength)
	otherCmd[0], otherCmd[1] = ackMarker, cmdSetEffect
	notAck := make([]byte, reportLength)
	notAck[1] = cmdWriteSlot
	f := &fakeTransport{respond: func(out []byte) [][]byte {
		return append([][]byte{short, otherCmd, notAck}, deviceAck(nil)(out)...)
	}}
	c := newTestClient(f)

	err := c.WriteSlot(context.Background(), 0, DisabledSlot())

	assert.NoError(t, err)
	assert.Empty(t, f.pending)
}

func TestClient_Exchange_WhenALateAckForAnotherPayloadArrives_ShouldNotTakeItAsConfirmation(t *testing.T) {
	t.Parallel()
	// Ack for an earlier write of F13 to slot 1, arriving after its
	// exchange gave up.
	stale := deviceAck(nil)([]byte{0x00, commandClass, cmdWriteSlot, lenWriteSlot, 0x04, 0x00, 0x00, 0x00, 0x00, 0x20, 0x00, 0x68, 0x00})[0]
	f := &fakeTransport{respond: func([]byte) [][]byte { return [][]byte{stale} }}
	c := newTestClient(f)

	err := c.WriteSlot(context.Background(), 0, KeyboardSlot(0x01, 0x04))

	assert.ErrorIs(t, err, ErrNoAck)
}

func TestClient_Exchange_WhenReadFails_ShouldWrapErrRead(t *testing.T) {
	t.Parallel()
	cause := errors.New("unplugged")
	c := newTestClient(&fakeTransport{readErr: cause})

	err := c.SetEffect(context.Background(), Effect{})

	assert.ErrorIs(t, err, ErrRead)
	assert.ErrorIs(t, err, cause)
}

// The factory reset (06 0F ..) and bootloader entry (5A ..) commands
// must never leave this package; every exported command uses class
// 0x06 with one of the four verified command bytes.
func TestClient_ExportedCommands_ShouldOnlySendVerifiedCommands(t *testing.T) {
	t.Parallel()
	f := &fakeTransport{respond: deviceAck(factorySlotData())}
	c := newTestClient(f)

	_, err := c.ReadSlots(context.Background())
	require.NoError(t, err)
	require.NoError(t, c.WriteSlot(context.Background(), 0, KeyboardSlot(0x01, 0x04)))
	require.NoError(t, c.SetEffect(context.Background(), Effect{Style: StyleSpectrum}))
	require.NoError(t, c.SetKeyColor(context.Background(), 0, RGB{}))

	allowed := map[byte]bool{cmdReadSlots: true, cmdWriteSlot: true, cmdSetEffect: true, cmdSetKeyColor: true}
	for _, raw := range f.writes {
		pkt := packet(t, raw)
		assert.Equal(t, commandClass, pkt[0])
		assert.True(t, allowed[pkt[1]], "unexpected command 0x%02x", pkt[1])
	}
}

func TestClient_ApplySlots_ShouldWriteOnlyTheSlotsThatDiffer(t *testing.T) {
	t.Parallel()
	current := factorySlotData()
	f := &fakeTransport{respond: deviceAck(current)}
	c := newTestClient(f)
	want, err := c.ReadSlots(context.Background())
	require.NoError(t, err)
	want[0] = KeyboardSlot(0x00, 0x68)
	want[19] = ConsumerSlot(0x00CD)
	f.writes = nil

	written, err := c.ApplySlots(context.Background(), want)

	require.NoError(t, err)
	assert.Equal(t, 2, written)
	require.Len(t, f.writes, 4, "two chunked reads, then one write per changed slot")
	assert.Equal(t, []byte{0x06, 0x10, 0x07, 0x00, 0x00}, packet(t, f.writes[2])[:5])
	assert.Equal(t, []byte{0x06, 0x10, 0x07, 0x4C, 0x00}, packet(t, f.writes[3])[:5])
}

func TestClient_ApplySlots_WhenAlreadyApplied_ShouldWriteNothing(t *testing.T) {
	t.Parallel()
	f := &fakeTransport{respond: deviceAck(factorySlotData())}
	c := newTestClient(f)
	want, err := c.ReadSlots(context.Background())
	require.NoError(t, err)

	written, err := c.ApplySlots(context.Background(), want)

	require.NoError(t, err)
	assert.Zero(t, written)
}

func TestClient_ApplySlots_WhenLengthDoesNotMatchLimits_ShouldReturnErrInvalidArgument(t *testing.T) {
	t.Parallel()
	f := &fakeTransport{}
	c := newTestClient(f)

	written, err := c.ApplySlots(context.Background(), make([]Slot, 3))

	assert.Zero(t, written)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	assert.Empty(t, f.writes)
}

func TestClient_ApplySlots_WhenReadFails_ShouldWrapError(t *testing.T) {
	t.Parallel()
	c := newTestClient(&fakeTransport{})

	_, err := c.ApplySlots(context.Background(), make([]Slot, testLimits.Slots))

	assert.ErrorIs(t, err, ErrNoAck)
}

func TestClient_ApplySlots_WhenAWriteFails_ShouldReportSlotsWrittenSoFar(t *testing.T) {
	t.Parallel()
	ack := deviceAck(factorySlotData())
	f := &fakeTransport{}
	f.respond = func(out []byte) [][]byte {
		// Acknowledge the reads and the first write only.
		if out[2] == cmdWriteSlot && out[4] != 0x00 {
			return nil
		}
		return ack(out)
	}
	c := newTestClient(f)
	want := make([]Slot, testLimits.Slots)
	for i := range want {
		want[i] = KeyboardSlot(0x00, 0x68)
	}

	written, err := c.ApplySlots(context.Background(), want)

	assert.Equal(t, 1, written)
	assert.ErrorIs(t, err, ErrNoAck)
}

func TestClient_WhenContextAlreadyDone_ShouldReturnCtxErrWithoutWriting(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fakeTransport{respond: deviceAck(factorySlotData())}
	c := newTestClient(f)

	_, readErr := c.ReadSlots(ctx)
	writeErr := c.WriteSlot(ctx, 0, DisabledSlot())
	effectErr := c.SetEffect(ctx, Effect{})
	colorErr := c.SetKeyColor(ctx, 0, RGB{})
	_, applyErr := c.ApplySlots(ctx, make([]Slot, testLimits.Slots))

	for _, err := range []error{readErr, writeErr, effectErr, colorErr, applyErr} {
		assert.ErrorIs(t, err, context.Canceled)
	}
	assert.Empty(t, f.writes)
}

func TestClient_WhenContextCancelledWhileWaitingForAck_ShouldReturnCtxErr(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	f := &fakeTransport{onRead: cancel}
	c := &Client{t: f, limits: testLimits, ackTimeout: time.Minute}

	err := c.WriteSlot(ctx, 0, DisabledSlot())

	assert.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, ErrNoAck)
	assert.Len(t, f.writes, 1)
}

func TestClient_WhenContextDeadlineExceededWhileWaitingForAck_ShouldReturnDeadlineExceeded(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	c := &Client{t: &fakeTransport{}, limits: testLimits, ackTimeout: time.Minute}

	err := c.SetEffect(ctx, Effect{})

	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestClient_ApplySlots_WhenContextCancelledBetweenSlots_ShouldStopAndReportWritten(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	ack := deviceAck(factorySlotData())
	f := &fakeTransport{}
	slotWrites := 0
	f.respond = func(out []byte) [][]byte {
		if out[2] == cmdWriteSlot {
			slotWrites++
			if slotWrites == 2 {
				cancel()
			}
		}
		return ack(out)
	}
	c := newTestClient(f)
	want := make([]Slot, testLimits.Slots)
	for i := range want {
		want[i] = KeyboardSlot(0x00, 0x68)
	}

	written, err := c.ApplySlots(ctx, want)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, written, "only the slot acked before cancellation counts")
	assert.Equal(t, 2, slotWrites, "no slot write is sent after cancellation")
}

func TestClient_WhenContextDoneAsAckTimeoutExpires_ShouldReturnCtxErrNotErrNoAck(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	f := &fakeTransport{respond: func([]byte) [][]byte {
		cancel()
		return nil
	}}
	// A zero ack window skips the poll loop, so only the post-loop check
	// can see the cancellation.
	c := &Client{t: f, limits: testLimits, ackTimeout: 0}

	err := c.WriteSlot(ctx, 0, DisabledSlot())

	assert.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, ErrNoAck)
}

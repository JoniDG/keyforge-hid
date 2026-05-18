package events

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/JoniDG/keyforge-hid/internal/device"
	"github.com/JoniDG/keyforge-protocol/go/protocol"
)

// EventSink consumes one InputEvent at a time. Returning a non-nil
// error cancels every reader and propagates the error back from
// StreamAll.
type EventSink func(protocol.InputEvent) error

// reportMapper is the polymorphic surface every per-role decoder
// implements. ErrShortReport is treated as a benign skip by the
// pipeline; any other non-nil error cancels reading.
type reportMapper interface {
	Map(report []byte) ([]protocol.InputEvent, error)
}

// StreamAll opens every input in parallel via opener, decodes each
// stream's reports through the role-appropriate mapper, and delivers
// the resulting InputEvents to sink in arrival order. It blocks until
// the parent context is cancelled, sink returns an error, or any
// reader fails with a non-cancellation error.
//
// Inputs whose Role is not handled by the package are skipped silently.
// When no inputs are decodable (empty list or every entry skipped),
// StreamAll returns nil immediately without opening anything.
//
// All opened streams are closed before StreamAll returns, even on the
// error path.
func StreamAll(parentCtx context.Context, opener device.Opener, deviceID protocol.DeviceID, inputs []device.MatchedInput, sink EventSink) error {
	streams, mappers, err := openAll(opener, deviceID, inputs)
	if err != nil {
		return err
	}
	defer closeAll(streams)

	if len(streams) == 0 {
		return nil
	}

	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	eventsCh := make(chan protocol.InputEvent, len(streams))
	readErrs := make(chan error, len(streams))

	var wg sync.WaitGroup
	for i := range streams {
		wg.Add(1)
		go runReader(ctx, &wg, streams[i], mappers[i], eventsCh, readErrs)
	}
	go func() {
		wg.Wait()
		close(eventsCh)
		close(readErrs)
	}()

	var sinkErr error
	for e := range eventsCh {
		if sinkErr != nil {
			continue
		}
		if err := sink(e); err != nil {
			sinkErr = err
			cancel()
		}
	}
	if sinkErr != nil {
		return sinkErr
	}
	for err := range readErrs {
		if err != nil {
			return err
		}
	}
	return parentCtx.Err()
}

func runReader(ctx context.Context, wg *sync.WaitGroup, stream device.InputStream, mapper reportMapper, events chan<- protocol.InputEvent, errs chan<- error) {
	defer wg.Done()
	err := stream.Read(ctx, func(report []byte) error {
		evs, mErr := mapper.Map(report)
		if mErr != nil {
			if errors.Is(mErr, ErrShortReport) {
				return nil
			}
			return mErr
		}
		for _, e := range evs {
			select {
			case events <- e:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		errs <- err
	}
}

func openAll(opener device.Opener, deviceID protocol.DeviceID, inputs []device.MatchedInput) ([]device.InputStream, []reportMapper, error) {
	streams := make([]device.InputStream, 0, len(inputs))
	mappers := make([]reportMapper, 0, len(inputs))
	for _, in := range inputs {
		mapper := mapperFor(in.Role, deviceID)
		if mapper == nil {
			continue
		}
		stream, err := opener.Open(in.Info.Path)
		if err != nil {
			closeAll(streams)
			return nil, nil, fmt.Errorf("events.StreamAll: open %q: %w", in.Info.Path, err)
		}
		streams = append(streams, stream)
		mappers = append(mappers, mapper)
	}
	return streams, mappers, nil
}

func closeAll(streams []device.InputStream) {
	for _, s := range streams {
		_ = s.Close()
	}
}

func mapperFor(role device.InputRole, deviceID protocol.DeviceID) reportMapper {
	switch role {
	case device.RoleKeyboard:
		return NewKeyboardMapper(deviceID)
	case device.RoleEncoder:
		return NewEncoderMapper(deviceID)
	default:
		return nil
	}
}

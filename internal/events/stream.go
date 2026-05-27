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

// readerSpec bundles an opened stream with every mapper that should
// process its reports. When two inputs declare the same platform path
// (HID multi-TLC interfaces report this on macOS), they collapse into
// a single readerSpec whose mappers are applied in declaration order
// to each incoming report.
type readerSpec struct {
	info    device.Info
	stream  device.InputStream
	mappers []reportMapper
}

// StreamAll opens every input in parallel via opener, decodes each
// stream's reports through the role-appropriate mapper(s), and
// delivers the resulting InputEvents to sink in arrival order. It
// blocks until the parent context is cancelled, sink returns an error,
// or any reader fails with a non-cancellation error.
//
// Inputs whose platform Path coincides (multi-TLC interfaces) are
// opened once and every report is offered to each mapper for that
// path: whichever mapper recognizes the report emits, the rest skip
// silently via ErrShortReport / empty-slice returns.
//
// Inputs whose Role is not handled by the package are skipped. When
// no inputs are decodable, StreamAll returns nil immediately without
// opening anything. All opened streams are closed before StreamAll
// returns, even on the error path.
func StreamAll(parentCtx context.Context, opener device.Opener, deviceID protocol.DeviceID, inputs []device.MatchedInput, sink EventSink) error {
	specs, err := openAll(opener, deviceID, inputs)
	if err != nil {
		return err
	}
	defer closeAllSpecs(specs)

	if len(specs) == 0 {
		return nil
	}

	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	eventsCh := make(chan protocol.InputEvent, len(specs))
	readErrs := make(chan error, len(specs))

	var wg sync.WaitGroup
	for i := range specs {
		wg.Add(1)
		go runReader(ctx, cancel, &wg, specs[i].stream, specs[i].mappers, eventsCh, readErrs)
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

func runReader(ctx context.Context, cancel context.CancelFunc, wg *sync.WaitGroup, stream device.InputStream, mappers []reportMapper, events chan<- protocol.InputEvent, errs chan<- error) {
	defer wg.Done()
	err := stream.Read(ctx, func(report []byte) error {
		for _, mapper := range mappers {
			evs, mErr := mapper.Map(report)
			if mErr != nil {
				if errors.Is(mErr, ErrShortReport) {
					continue
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
		}
		return nil
	})
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		errs <- err
		// Stop the sibling readers so wg.Wait can complete and eventsCh
		// can close; otherwise readers still blocked on ctx would hang
		// StreamAll forever instead of letting this error propagate.
		cancel()
	}
}

func openAll(opener device.Opener, deviceID protocol.DeviceID, inputs []device.MatchedInput) ([]readerSpec, error) {
	indexByPath := make(map[string]int)
	specs := make([]readerSpec, 0, len(inputs))
	for _, in := range inputs {
		mapper := mapperFor(in.Role, deviceID)
		if mapper == nil {
			continue
		}
		if idx, seen := indexByPath[in.Info.Path]; seen {
			specs[idx].mappers = append(specs[idx].mappers, mapper)
			continue
		}
		indexByPath[in.Info.Path] = len(specs)
		specs = append(specs, readerSpec{info: in.Info, mappers: []reportMapper{mapper}})
	}

	for i := range specs {
		stream, err := opener.Open(specs[i].info.Path)
		if err != nil {
			closeAllSpecs(specs[:i])
			return nil, fmt.Errorf("events.StreamAll: open %q: %w", specs[i].info.Path, err)
		}
		specs[i].stream = stream
	}
	return specs, nil
}

func closeAllSpecs(specs []readerSpec) {
	for _, s := range specs {
		if s.stream != nil {
			_ = s.stream.Close()
		}
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

package device

import (
	"errors"
	"fmt"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOverlapped_MarkAccessDenied(t *testing.T) {
	t.Parallel()
	other := errors.New("device not connected")
	tests := []struct {
		name       string
		err        error
		wantDenied bool
	}{
		{name: "WhenAccessDenied_ShouldTag", err: winErrorAccessDenied, wantDenied: true},
		{name: "WhenWrappedAccessDenied_ShouldTag", err: fmt.Errorf("CreateFile: %w", winErrorAccessDenied), wantDenied: true},
		{name: "WhenOtherErrno_ShouldNotTag", err: syscall.Errno(1167)},
		{name: "WhenNotErrno_ShouldNotTag", err: other},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := markAccessDenied(tt.err)

			assert.ErrorIs(t, got, tt.err)
			assert.Equal(t, tt.wantDenied, errors.Is(got, ErrAccessDenied))
		})
	}
}

func TestOverlapped_StripZeroReportID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		report []byte
		want   []byte
	}{
		{name: "WhenZeroID_ShouldDropIt", report: []byte{0x00, 0x01, 0x02}, want: []byte{0x01, 0x02}},
		{name: "WhenNumberedReport_ShouldKeepIt", report: []byte{0x03, 0x08, 0x00}, want: []byte{0x03, 0x08, 0x00}},
		{name: "WhenEmpty_ShouldReturnEmpty", report: []byte{}, want: []byte{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, stripZeroReportID(tt.report))
		})
	}
}

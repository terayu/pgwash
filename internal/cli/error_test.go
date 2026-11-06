package cli

import (
	"errors"
	"testing"
)

func TestNewCodedErrorNilErrReturnsNil(t *testing.T) {
	if err := NewCodedError(ExitPhaseAFailed, nil); err != nil {
		t.Errorf("NewCodedError(code, nil) = %v, want nil", err)
	}
}

func TestNewCodedErrorWraps(t *testing.T) {
	base := errors.New("boom")
	err := NewCodedError(ExitPhaseBFailed, base)

	var ce *CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("errors.As failed to extract *CodedError from %v", err)
	}
	if ce.Code != ExitPhaseBFailed {
		t.Errorf("Code = %d, want %d", ce.Code, ExitPhaseBFailed)
	}
	if !errors.Is(err, base) {
		t.Errorf("errors.Is(err, base) = false, want true (Unwrap should expose base error)")
	}
}

func TestHandleExecError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil error", nil, ExitOK},
		{"plain error", errors.New("bad flag"), ExitConfigError},
		{"coded config error", NewCodedError(ExitConfigError, errors.New("x")), ExitConfigError},
		{"coded phase-a failed", NewCodedError(ExitPhaseAFailed, errors.New("x")), ExitPhaseAFailed},
		{"coded phase-b failed", NewCodedError(ExitPhaseBFailed, errors.New("x")), ExitPhaseBFailed},
		{"coded manual wait", NewCodedError(ExitPhaseAWaitingManual, errors.New("x")), ExitPhaseAWaitingManual},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := handleExecError(c.err)
			if got != c.want {
				t.Errorf("handleExecError(%v) = %d, want %d", c.err, got, c.want)
			}
		})
	}
}

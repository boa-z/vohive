package device

import (
	"context"
	"testing"
	"time"

	qmimanager "github.com/boa-z/quectel-qmi-go/pkg/manager"
)

type mockStartupUIMResetter struct {
	called int
	err    error
}

func (m *mockStartupUIMResetter) UIMReset(ctx context.Context) error {
	m.called++
	return m.err
}

type mockStartupProvisioningEnsurer struct {
	called int
	err    error
}

func (m *mockStartupProvisioningEnsurer) EnsureSIMProvisioned(ctx context.Context, opts qmimanager.EnsureSIMProvisionedOptions) (qmimanager.UIMReadiness, error) {
	m.called++
	return qmimanager.UIMReadiness{}, m.err
}

func TestPerformStartupQMIUIMReset(t *testing.T) {
	resetter := &mockStartupUIMResetter{}
	ensurer := &mockStartupProvisioningEnsurer{}
	readyCheckCalled := 0
	readyCheck := func(ctx context.Context) (bool, error) {
		readyCheckCalled++
		return true, nil
	}

	res := performStartupQMIUIMReset("dev1", resetter, ensurer, readyCheck, time.Millisecond*50, time.Millisecond*10)
	if !res {
		t.Fatalf("expected true, got false")
	}

	if resetter.called != 1 {
		t.Errorf("expected resetter called 1 time, got %d", resetter.called)
	}

	if ensurer.called != 1 {
		t.Errorf("expected ensurer called 1 time, got %d", ensurer.called)
	}

	if readyCheckCalled != 1 {
		t.Errorf("expected readyCheck called 1 time, got %d", readyCheckCalled)
	}
}

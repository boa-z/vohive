package device

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zanescope/vohive/internal/config"
)

func TestMissingWorkerRetryUsesCappedBackoff(t *testing.T) {
	p := NewPool(&config.Config{})
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	wantDelays := []time.Duration{
		time.Minute,
		2 * time.Minute,
		5 * time.Minute,
		10 * time.Minute,
		30 * time.Minute,
		30 * time.Minute,
	}

	for i, wantDelay := range wantDelays {
		due, attempt, delay := p.reserveMissingWorkerRetry("wwan11", now)
		if !due {
			t.Fatalf("attempt %d was not due", i+1)
		}
		if attempt != i+1 {
			t.Fatalf("attempt = %d, want %d", attempt, i+1)
		}
		if delay != wantDelay {
			t.Fatalf("attempt %d delay = %s, want %s", attempt, delay, wantDelay)
		}

		due, _, remaining := p.reserveMissingWorkerRetry("wwan11", now.Add(delay/2))
		if due {
			t.Fatalf("attempt %d became due inside backoff window", attempt)
		}
		if remaining != delay/2 {
			t.Fatalf("attempt %d remaining = %s, want %s", attempt, remaining, delay/2)
		}
		now = now.Add(delay)
	}

	p.clearMissingWorkerRetry("wwan11")
	due, attempt, delay := p.reserveMissingWorkerRetry("wwan11", now)
	if !due || attempt != 1 || delay != time.Minute {
		t.Fatalf("retry state was not reset: due=%v attempt=%d delay=%s", due, attempt, delay)
	}
}

func TestMissingWorkerRecoveryProcessesAllDevicesWithinLimit(t *testing.T) {
	ids := []string{"dev1", "dev2", "dev3"}
	var raw strings.Builder
	raw.WriteString("free_device_limit: 2\ndevices:\n")
	for i, id := range ids {
		fmt.Fprintf(&raw, "- id: %s\n  device_backend: at\n  modem_imei: \"%015d\"\n", id, i+1)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(raw.String()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := config.InitGlobalManager(configPath); err != nil {
		t.Fatalf("InitGlobalManager() error = %v", err)
	}

	p := NewPool(&config.Config{FreeDeviceLimit: 2})
	defer p.cancel()
	if needRescan := p.runHealthCheckTick(); !needRescan {
		t.Fatal("runHealthCheckTick() = false, want rescan for missing configured devices")
	}

	p.missingWorkerRetryMu.Lock()
	defer p.missingWorkerRetryMu.Unlock()
	for _, id := range ids[:2] {
		state, ok := p.missingWorkerRetries[id]
		if !ok || state.Attempts != 1 {
			t.Fatalf("retry state for %s = %+v, present=%v; want first attempt", id, state, ok)
		}
	}
	if _, ok := p.missingWorkerRetries[ids[2]]; ok {
		t.Fatalf("device %s outside free-device limit received a retry", ids[2])
	}
}

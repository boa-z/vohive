package device

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zanescope/vohive/internal/config"
)

func initRescanSafetyConfig(t *testing.T, dev config.DeviceConfig) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	raw := "devices:\n- id: " + dev.ID + "\n  device_backend: qmi\n  modem_imei: \"" + dev.ModemIMEI + "\"\n  control_device: " + dev.ControlDevice + "\n  interface: " + dev.Interface + "\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := config.InitGlobalManager(configPath); err != nil {
		t.Fatalf("InitGlobalManager() error = %v", err)
	}
}

func registerRescanSafetyWorker(p *Pool, dev config.DeviceConfig) *Worker {
	w := &Worker{
		ID:     dev.ID,
		Config: dev,
		Pool:   p,
		stop:   make(chan struct{}),
	}
	p.mu.Lock()
	p.workers[dev.ID] = w
	p.mu.Unlock()
	return w
}

func TestRescanScanErrorDoesNotMutateWorkers(t *testing.T) {
	p := NewPool(&config.Config{})
	defer p.cancel()
	dev := config.DeviceConfig{ID: "dev-scan-error", DeviceBackend: "qmi", ModemIMEI: "111111111111111"}
	w := registerRescanSafetyWorker(p, dev)

	origDiscover := discoverQMIDevicesFn
	discoverQMIDevicesFn = func() ([]QMIDevice, error) {
		return nil, errors.New("sysfs unavailable")
	}
	t.Cleanup(func() { discoverQMIDevicesFn = origDiscover })

	if err := p.RescanAndReconnect(); err == nil {
		t.Fatal("RescanAndReconnect() error = nil, want discovery error")
	}
	if got := p.GetWorker(dev.ID); got != w {
		t.Fatalf("worker changed after failed discovery: got %p want %p", got, w)
	}
}

func TestRescanMatchedWorkerIgnoresSingleProbeFailure(t *testing.T) {
	dev := config.DeviceConfig{
		ID:            "dev-matched",
		DeviceBackend: "qmi",
		ModemIMEI:     "222222222222222",
		ControlDevice: "/dev/cdc-wdm0",
		Interface:     "wwan0",
		USBPath:       "/sys/bus/usb/devices/1-2",
	}
	initRescanSafetyConfig(t, dev)
	p := NewPool(&config.Config{Devices: []config.DeviceConfig{dev}})
	defer p.cancel()
	w := registerRescanSafetyWorker(p, dev)

	origDiscover := discoverQMIDevicesFn
	discoverQMIDevicesFn = func() ([]QMIDevice, error) {
		return []QMIDevice{{
			ControlPath:  dev.ControlDevice,
			NetInterface: dev.Interface,
			USBPath:      dev.USBPath,
		}}, nil
	}
	t.Cleanup(func() { discoverQMIDevicesFn = origDiscover })

	if healthy := w.IsDeviceHealthy(); healthy {
		t.Fatal("test worker unexpectedly healthy; regression requires a failed one-shot probe")
	}
	if err := p.RescanAndReconnect(); err != nil {
		t.Fatalf("RescanAndReconnect() error = %v", err)
	}
	if got := p.GetWorker(dev.ID); got != w {
		t.Fatalf("matched worker was rebuilt after one failed probe: got %p want %p", got, w)
	}
}

func TestRescanMissingHealthyWorkerIsNotEvicted(t *testing.T) {
	dev := config.DeviceConfig{
		ID:            "dev-missing",
		DeviceBackend: "qmi",
		ModemIMEI:     "333333333333333",
		ControlDevice: "/dev/cdc-wdm11",
		Interface:     "wwan11",
	}
	initRescanSafetyConfig(t, dev)
	p := NewPool(&config.Config{Devices: []config.DeviceConfig{dev}})
	defer p.cancel()
	w := registerRescanSafetyWorker(p, dev)

	origDiscover := discoverQMIDevicesFn
	discoverQMIDevicesFn = func() ([]QMIDevice, error) { return nil, nil }
	t.Cleanup(func() { discoverQMIDevicesFn = origDiscover })

	if got := w.HealthSnapshot().State; got != HealthStateHealthy {
		t.Fatalf("initial health state = %s, want %s", got, HealthStateHealthy)
	}
	if err := p.RescanAndReconnect(); err != nil {
		t.Fatalf("RescanAndReconnect() error = %v", err)
	}
	if got := p.GetWorker(dev.ID); got != w {
		t.Fatalf("healthy worker was evicted only because discovery missed it: got %p want %p", got, w)
	}
}

func TestScheduleRescanCoalescesBurstWithoutOverlap(t *testing.T) {
	p := NewPool(&config.Config{})
	defer p.cancel()

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	completed := make(chan struct{}, 2)
	var calls atomic.Int32
	var active atomic.Int32
	var maxActive atomic.Int32
	p.rescanAndReconnectForTest = func() error {
		call := calls.Add(1)
		current := active.Add(1)
		for {
			previous := maxActive.Load()
			if current <= previous || maxActive.CompareAndSwap(previous, current) {
				break
			}
		}
		if call == 1 {
			close(firstStarted)
			<-releaseFirst
		}
		active.Add(-1)
		completed <- struct{}{}
		return nil
	}

	p.scheduleRescan("health_check")
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first scheduled rescan did not start")
	}
	for i := 0; i < 50; i++ {
		p.scheduleRescan("udev")
	}
	close(releaseFirst)

	for i := 0; i < 2; i++ {
		select {
		case <-completed:
		case <-time.After(time.Second):
			t.Fatalf("scheduled rescan %d did not complete", i+1)
		}
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("rescan calls = %d, want exactly current + one coalesced follow-up", got)
	}
	if got := maxActive.Load(); got != 1 {
		t.Fatalf("overlapping rescans = %d, want 1", got)
	}
}

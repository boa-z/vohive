package device

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zanescope/vohive/internal/config"
)

// TestPoolAddWorkerFromConfigRejectsFourthWorkerBeforeHardwareInit 测试当设备数量达到限制时，添加新设备应该被限额策略拒绝
func TestPoolAddWorkerFromConfigRejectsFourthWorkerBeforeHardwareInit(t *testing.T) {
	p := NewPool(&config.Config{FreeDeviceLimit: DefaultFreeDeviceLimit})
	for i := 1; i <= DefaultFreeDeviceLimit; i++ {
		id := fmt.Sprintf("dev%d", i)
		p.workers[id] = &Worker{ID: id, Config: config.DeviceConfig{ID: id}}
	}

	// 使用超过限制的额外设备 ID（如 dev5），触发限额策略
	extraID := fmt.Sprintf("dev%d", DefaultFreeDeviceLimit+1)
	_, err := p.AddWorkerFromConfig(config.DeviceConfig{ID: extraID})
	if err == nil {
		t.Fatal("AddWorkerFromConfig() error = nil, want device limit error")
	}
	if !strings.Contains(err.Error(), FreeDeviceWorkerLimitMessage(p.FreeDeviceLimit())) {
		t.Fatalf("AddWorkerFromConfig() error = %q, want %q", err.Error(), FreeDeviceWorkerLimitMessage(p.FreeDeviceLimit()))
	}
	if len(p.workers) != DefaultFreeDeviceLimit {
		t.Fatalf("worker count = %d, want %d", len(p.workers), DefaultFreeDeviceLimit)
	}
}

// TestPoolAddWorkerFromConfigKeepsExistingDeviceErrorBeforeLimitError 测试即使达到设备限制，当尝试添加一个已存在的同名设备时，应该优先返回“设备已存在”错误而非限制错误
func TestPoolAddWorkerFromConfigKeepsExistingDeviceErrorBeforeLimitError(t *testing.T) {
	p := NewPool(&config.Config{FreeDeviceLimit: DefaultFreeDeviceLimit})
	for i := 1; i <= DefaultFreeDeviceLimit; i++ {
		id := fmt.Sprintf("dev%d", i)
		p.workers[id] = &Worker{ID: id, Config: config.DeviceConfig{ID: id}}
	}

	_, err := p.AddWorkerFromConfig(config.DeviceConfig{ID: "dev1"})
	if err == nil {
		t.Fatal("AddWorkerFromConfig() error = nil, want existing device error")
	}
	if !strings.Contains(err.Error(), "设备已存在") {
		t.Fatalf("AddWorkerFromConfig() error = %q, want existing device error", err.Error())
	}
}

// TestFreeDeviceLimitAllowsRebuildAfterRemovingWorker 测试移除某个设备后，已使用的配额应被释放，从而允许重新添加/启动设备
func TestFreeDeviceLimitAllowsRebuildAfterRemovingWorker(t *testing.T) {
	p := NewPool(&config.Config{FreeDeviceLimit: DefaultFreeDeviceLimit})
	for i := 1; i <= DefaultFreeDeviceLimit; i++ {
		id := fmt.Sprintf("dev%d", i)
		p.workers[id] = &Worker{ID: id, Config: config.DeviceConfig{ID: id}}
	}
	if err := p.RemoveWorker("dev1"); err != nil {
		t.Fatalf("RemoveWorker() error = %v", err)
	}
	if FreeDeviceLimitReached(len(p.workers), p.FreeDeviceLimit()) {
		t.Fatalf("FreeDeviceLimitReached(%d) = true, want false after removal", len(p.workers))
	}
}

// TestRemoveWorkerWaitsForInProgressInitialization 测试移除正在初始化中的设备时，应该同步等待其初始化完成后再执行销毁流程
func TestRemoveWorkerWaitsForInProgressInitialization(t *testing.T) {
	p := NewPool(&config.Config{})
	p.rebuilding["dev1"] = true

	go func() {
		time.Sleep(20 * time.Millisecond)
		p.mu.Lock()
		p.workers["dev1"] = &Worker{
			ID:     "dev1",
			Config: config.DeviceConfig{ID: "dev1"},
			stop:   make(chan struct{}),
		}
		delete(p.rebuilding, "dev1")
		p.mu.Unlock()
	}()

	if err := p.RemoveWorker("dev1"); err != nil {
		t.Fatalf("RemoveWorker() error = %v, want nil after in-progress init finishes", err)
	}
	if worker := p.GetWorker("dev1"); worker != nil {
		t.Fatalf("worker still exists after RemoveWorker: %#v", worker)
	}
}

// TestBeginRebuildAttemptLockedIncrementsMonotonically 测试同一设备连续两次进入启动流程时 token 单调递增
func TestBeginRebuildAttemptLockedIncrementsMonotonically(t *testing.T) {
	p := NewPool(&config.Config{})
	p.mu.Lock()
	first := p.beginRebuildAttemptLocked("dev1")
	second := p.beginRebuildAttemptLocked("dev1")
	p.mu.Unlock()

	if first != 1 {
		t.Fatalf("first attempt token = %d, want 1", first)
	}
	if second != 2 {
		t.Fatalf("second attempt token = %d, want 2", second)
	}
}

// TestEndRebuildAttemptIfCurrentOnlyClearsMatchingToken 测试只有 token 与最新一次尝试匹配时才会清除 rebuilding 标记，
// 避免滞后完成的旧启动流程误清新一轮尝试的状态
func TestEndRebuildAttemptIfCurrentOnlyClearsMatchingToken(t *testing.T) {
	p := NewPool(&config.Config{})
	p.mu.Lock()
	p.rebuilding["dev1"] = true
	p.rebuildAttempt["dev1"] = 2
	p.mu.Unlock()

	p.endRebuildAttemptIfCurrent("dev1", 1)
	p.mu.RLock()
	stillRebuilding := p.rebuilding["dev1"]
	p.mu.RUnlock()
	if !stillRebuilding {
		t.Fatal("stale token cleared rebuilding flag, want untouched")
	}

	p.endRebuildAttemptIfCurrent("dev1", 2)
	p.mu.RLock()
	stillRebuilding = p.rebuilding["dev1"]
	p.mu.RUnlock()
	if stillRebuilding {
		t.Fatal("current token failed to clear rebuilding flag")
	}
}

// TestStartBootstrapWatchdogForceClearsRebuildingAfterDeadline 测试启动看门狗在截止时间到达后，
// 如果该尝试仍是设备最新一次尝试，会强制释放 rebuilding 标记
func TestStartBootstrapWatchdogForceClearsRebuildingAfterDeadline(t *testing.T) {
	p := NewPool(&config.Config{})
	defer p.cancel()
	p.mu.Lock()
	p.rebuilding["dev1"] = true
	p.rebuildAttempt["dev1"] = 1
	p.mu.Unlock()

	stop := p.startBootstrapWatchdog("dev1", 1, 20*time.Millisecond)
	defer close(stop)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.RLock()
		cleared := !p.rebuilding["dev1"]
		p.mu.RUnlock()
		if cleared {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("watchdog did not clear rebuilding flag after deadline")
}

// TestStartBootstrapWatchdogIgnoresSupersededAttempt 测试看门狗触发时如果设备已经进入更新一轮尝试，
// 不应该误清新一轮尝试的 rebuilding 标记
func TestStartBootstrapWatchdogIgnoresSupersededAttempt(t *testing.T) {
	p := NewPool(&config.Config{})
	defer p.cancel()
	p.mu.Lock()
	p.rebuilding["dev1"] = true
	p.rebuildAttempt["dev1"] = 2 // 一次更新的尝试已经在进行
	p.mu.Unlock()

	stop := p.startBootstrapWatchdog("dev1", 1, 20*time.Millisecond)
	defer close(stop)

	time.Sleep(100 * time.Millisecond)

	p.mu.RLock()
	stillRebuilding := p.rebuilding["dev1"]
	p.mu.RUnlock()
	if !stillRebuilding {
		t.Fatal("watchdog cleared rebuilding flag for a superseded attempt, want untouched")
	}
}

// TestStartBootstrapWatchdogStopsWhenSignaled 测试正常完成路径 close(stop) 后看门狗不应该再触发
func TestStartBootstrapWatchdogStopsWhenSignaled(t *testing.T) {
	p := NewPool(&config.Config{})
	defer p.cancel()
	p.mu.Lock()
	p.rebuilding["dev1"] = true
	p.rebuildAttempt["dev1"] = 1
	p.mu.Unlock()

	stop := p.startBootstrapWatchdog("dev1", 1, 30*time.Millisecond)
	close(stop)

	time.Sleep(100 * time.Millisecond)

	p.mu.RLock()
	stillRebuilding := p.rebuilding["dev1"]
	p.mu.RUnlock()
	if !stillRebuilding {
		t.Fatal("watchdog fired after being stopped, want rebuilding flag untouched")
	}
}

func TestFreeDeviceLimitPolicySupportsConfiguredAndUnlimited(t *testing.T) {
	devices := []config.DeviceConfig{
		{ID: "dev1"},
		{ID: "dev2"},
		{ID: "dev3"},
	}

	configured := NewPool(&config.Config{FreeDeviceLimit: 2})
	if got := configured.FreeDeviceLimit(); got != 2 {
		t.Fatalf("FreeDeviceLimit() = %d, want 2", got)
	}
	if FreeDeviceLimitReached(1, configured.FreeDeviceLimit()) {
		t.Fatal("configured limit reached before boundary")
	}
	if !FreeDeviceLimitReached(2, configured.FreeDeviceLimit()) {
		t.Fatal("configured limit not reached at boundary")
	}
	if FreeDeviceLimitAllowsConfiguredDevice(devices, "dev3", configured.FreeDeviceLimit()) {
		t.Fatal("third configured device allowed with limit 2")
	}
	if got := FreeDeviceWorkerLimitMessage(configured.FreeDeviceLimit()); !strings.Contains(got, "2") {
		t.Fatalf("configured limit message = %q, want limit 2", got)
	}

	unlimited := NewPool(&config.Config{FreeDeviceLimit: 0})
	if got := unlimited.FreeDeviceLimit(); got != 0 {
		t.Fatalf("FreeDeviceLimit() = %d, want unlimited sentinel 0", got)
	}
	if FreeDeviceLimitReached(1000, unlimited.FreeDeviceLimit()) {
		t.Fatal("unlimited policy reported limit reached")
	}
	if !FreeDeviceLimitAllowsConfiguredDevice(devices, "dev3", unlimited.FreeDeviceLimit()) {
		t.Fatal("unlimited policy rejected configured device")
	}
}

func TestPoolDeviceLimitCountsInFlightStartupReservations(t *testing.T) {
	p := NewPool(&config.Config{FreeDeviceLimit: 1})
	defer p.cancel()
	p.mu.Lock()
	p.rebuilding["dev1"] = true
	p.rebuildAttempt["dev1"] = 1
	p.mu.Unlock()

	_, err := p.AddWorkerFromConfig(config.DeviceConfig{ID: "dev2"})
	if err == nil || !strings.Contains(err.Error(), FreeDeviceWorkerLimitMessage(1)) {
		t.Fatalf("AddWorkerFromConfig() error = %v, want in-flight device limit error", err)
	}
	p.mu.RLock()
	dev2Reserved := p.rebuilding["dev2"]
	p.mu.RUnlock()
	if dev2Reserved {
		t.Fatal("rejected worker unexpectedly reserved a startup slot")
	}
}

func TestOccupiedDeviceSlotsDoesNotDoubleCountRegisteredWorker(t *testing.T) {
	p := NewPool(&config.Config{FreeDeviceLimit: 2})
	defer p.cancel()
	p.mu.Lock()
	p.workers["dev1"] = &Worker{ID: "dev1"}
	p.rebuilding["dev1"] = true

	occupied := p.occupiedDeviceSlotsLocked()
	p.mu.Unlock()
	if occupied != 1 {
		t.Fatalf("occupiedDeviceSlotsLocked() = %d, want 1", occupied)
	}

	p.mu.Lock()
	p.rebuilding["dev2"] = true
	occupied = p.occupiedDeviceSlotsLocked()
	p.mu.Unlock()
	if occupied != 2 {
		t.Fatalf("occupiedDeviceSlotsLocked() = %d, want 2", occupied)
	}
}

func TestBootstrapWatchdogInvalidatesTimedOutAttemptBeforeRegistration(t *testing.T) {
	p := NewPool(&config.Config{FreeDeviceLimit: 1})
	defer p.cancel()
	p.mu.Lock()
	p.rebuilding["dev1"] = true
	p.rebuildAttempt["dev1"] = 1
	p.mu.Unlock()

	stop := p.startBootstrapWatchdog("dev1", 1, 20*time.Millisecond)
	defer close(stop)
	if !p.waitWorkerInitSettled("dev1", 2*time.Second) {
		t.Fatal("watchdog did not release timed-out startup reservation")
	}
	if p.isRebuildAttemptCurrent("dev1", 1) {
		t.Fatal("timed-out startup attempt remained current")
	}
	if err := p.registerWorkerStartingForAttempt(&Worker{ID: "dev1"}, 1); err == nil {
		t.Fatal("timed-out startup attempt registered a worker")
	}
	if worker := p.GetWorker("dev1"); worker != nil {
		t.Fatalf("timed-out startup registered worker: %#v", worker)
	}
}

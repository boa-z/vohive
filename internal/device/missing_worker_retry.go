package device

import (
	"strings"
	"time"

	"github.com/zanescope/vohive/internal/config"
)

const missingWorkerLogInterval = 30 * time.Minute

type missingWorkerRetryState struct {
	Attempts    int
	NextAttempt time.Time
}

func missingWorkerRetryDelay(attempt int) time.Duration {
	switch attempt {
	case 1:
		return time.Minute
	case 2:
		return 2 * time.Minute
	case 3:
		return 5 * time.Minute
	case 4:
		return 10 * time.Minute
	default:
		return 30 * time.Minute
	}
}

// reserveMissingWorkerRetry 原子地判断本轮是否允许重试，并提前保留下次时间窗。
// 提前预留可避免并发健康触发为同一个缺失设备发起重复恢复。
func (p *Pool) reserveMissingWorkerRetry(deviceID string, now time.Time) (bool, int, time.Duration) {
	if p == nil {
		return false, 0, 0
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return false, 0, 0
	}
	if now.IsZero() {
		now = time.Now()
	}

	p.missingWorkerRetryMu.Lock()
	defer p.missingWorkerRetryMu.Unlock()
	if p.missingWorkerRetries == nil {
		p.missingWorkerRetries = make(map[string]missingWorkerRetryState)
	}
	state := p.missingWorkerRetries[deviceID]
	if !state.NextAttempt.IsZero() && now.Before(state.NextAttempt) {
		return false, state.Attempts, state.NextAttempt.Sub(now)
	}
	state.Attempts++
	delay := missingWorkerRetryDelay(state.Attempts)
	state.NextAttempt = now.Add(delay)
	p.missingWorkerRetries[deviceID] = state
	return true, state.Attempts, delay
}

func (p *Pool) clearMissingWorkerRetry(deviceID string) {
	if p == nil {
		return
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return
	}
	p.missingWorkerRetryMu.Lock()
	delete(p.missingWorkerRetries, deviceID)
	p.missingWorkerRetryMu.Unlock()
}

func (p *Pool) pruneMissingWorkerRetries(managed []config.DeviceConfig) {
	if p == nil {
		return
	}
	configured := make(map[string]struct{}, len(managed))
	for _, dev := range managed {
		if id := strings.TrimSpace(dev.ID); id != "" {
			configured[id] = struct{}{}
		}
	}

	p.missingWorkerRetryMu.Lock()
	for deviceID := range p.missingWorkerRetries {
		if _, ok := configured[deviceID]; !ok {
			delete(p.missingWorkerRetries, deviceID)
		}
	}
	p.missingWorkerRetryMu.Unlock()
}

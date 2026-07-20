package device

import (
	"fmt"
	"strings"

	"github.com/zanescope/vohive/internal/config"
)

const DefaultFreeDeviceLimit = config.DefaultFreeDeviceLimit

func NormalizeFreeDeviceLimit(limit int) int {
	if limit <= 0 {
		return 0
	}
	return limit
}

func (p *Pool) FreeDeviceLimit() int {
	if p == nil || p.cfg == nil {
		return DefaultFreeDeviceLimit
	}
	return NormalizeFreeDeviceLimit(p.cfg.FreeDeviceLimit)
}

// occupiedDeviceSlotsLocked returns the number of unique worker IDs that are
// either registered or currently reserving a startup slot. p.mu must be held.
func (p *Pool) occupiedDeviceSlotsLocked() int {
	occupied := len(p.workers)
	for deviceID, rebuilding := range p.rebuilding {
		if !rebuilding {
			continue
		}
		if _, registered := p.workers[deviceID]; !registered {
			occupied++
		}
	}
	return occupied
}

func FreeDeviceLimitReached(count, limit int) bool {
	limit = NormalizeFreeDeviceLimit(limit)
	return limit > 0 && count >= limit
}

func FreeDeviceAddLimitMessage(limit int) string {
	return fmt.Sprintf("当前版本最多只能添加 %d 个设备", NormalizeFreeDeviceLimit(limit))
}

func FreeDeviceWorkerLimitMessage(limit int) string {
	return fmt.Sprintf("当前版本最多只能启动 %d 个设备", NormalizeFreeDeviceLimit(limit))
}

func FreeDeviceLimitAllowsConfiguredDevice(devices []config.DeviceConfig, deviceID string, limit int) bool {
	limit = NormalizeFreeDeviceLimit(limit)
	if limit == 0 {
		return true
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return true
	}
	seen := 0
	for _, dev := range devices {
		id := strings.TrimSpace(dev.ID)
		if id == "" {
			continue
		}
		seen++
		if id == deviceID {
			return seen <= limit
		}
	}
	return true
}

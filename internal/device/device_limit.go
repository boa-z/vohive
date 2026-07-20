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

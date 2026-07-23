package device

import (
	"strings"
	"time"

	"github.com/zanescope/vohive/internal/config"
	"github.com/zanescope/vohive/pkg/logger"
)

func (p *Pool) recoverMissingConfiguredWorkers(workerCount int) bool {
	managed := config.ListDevices()
	p.pruneMissingWorkerRetries(managed)
	limit := p.FreeDeviceLimit()
	now := time.Now()
	needRescan := false

	var qmiList []QMIDevice
	qmiDiscoveryDone := false
	qmiDiscoveryAvailable := false

	for _, md := range managed {
		if !FreeDeviceLimitAllowsConfiguredDevice(managed, md.ID, limit) {
			p.clearMissingWorkerRetry(md.ID)
			continue
		}

		p.mu.RLock()
		isRebuilding := p.rebuilding[md.ID]
		isRebootRecovering := p.modemRebootRecovering[md.ID]
		hasWorker := p.workers[md.ID] != nil
		p.mu.RUnlock()

		if hasWorker {
			p.clearMissingWorkerRetry(md.ID)
			continue
		}
		if strings.TrimSpace(md.ModemIMEI) == "" {
			p.clearMissingWorkerRetry(md.ID)
			continue
		}
		if isRebuilding || isRebootRecovering {
			continue
		}

		due, attempt, nextRetry := p.reserveMissingWorkerRetry(md.ID, now)
		if !due {
			continue
		}

		isQMIConf := strings.EqualFold(strings.TrimSpace(md.DeviceBackend), "qmi") ||
			(strings.TrimSpace(md.DeviceBackend) == "" && strings.TrimSpace(md.ControlDevice) != "")
		if isQMIConf && strings.TrimSpace(md.ControlDevice) != "" && strings.TrimSpace(md.Interface) != "" {
			if !qmiDiscoveryDone {
				qmiDiscoveryDone = true
				var err error
				qmiList, err = discoverQMIDevicesFn()
				qmiDiscoveryAvailable = err == nil
			}

			live := QMIDevice{}
			if qmiDiscoveryAvailable {
				for _, candidate := range qmiList {
					if strings.TrimSpace(candidate.ControlPath) == strings.TrimSpace(md.ControlDevice) ||
						strings.TrimSpace(candidate.NetInterface) == strings.TrimSpace(md.Interface) ||
						strings.TrimSpace(candidate.USBPath) == strings.TrimSpace(md.USBPath) {
						live = candidate
						break
					}
				}
			}

			if !shouldFastStartMissingQMIWorker(md, live, qmiDiscoveryAvailable) {
				logger.InfoRate("missing_worker_rescan:"+md.ID, missingWorkerLogInterval,
					"定时检查发现 QMI Worker 缺失，按退避计划触发全量重扫",
					"device", md.ID,
					"attempt", attempt,
					"next_retry_in", nextRetry.String())
				needRescan = true
				continue
			}

			logger.InfoRate("missing_worker_fast_start:"+md.ID, missingWorkerLogInterval,
				"定时检查发现免扫类型节点缺少 Worker，按退避计划尝试初始化",
				"device", md.ID,
				"attempt", attempt,
				"next_retry_in", nextRetry.String())
			go func(c config.DeviceConfig, retryDelay time.Duration) {
				if _, err := p.AddWorkerFromConfig(c); err != nil {
					logger.WarnRate("missing_worker_fast_start_failed:"+c.ID, missingWorkerLogInterval,
						"快速拉起缺失节点失败，将按退避计划重试",
						"device", c.ID,
						"next_retry_in", retryDelay.String(),
						"err", err)
					return
				}
				p.clearMissingWorkerRetry(c.ID)
			}(md, nextRetry)
			continue
		}

		logger.InfoRate("missing_worker_rescan:"+md.ID, missingWorkerLogInterval,
			"定时检查发现已配置设备缺少 Worker，按退避计划触发重连扫描",
			"device", md.ID,
			"imei", md.ModemIMEI,
			"active_workers", workerCount,
			"attempt", attempt,
			"next_retry_in", nextRetry.String())
		needRescan = true
	}

	return needRescan
}

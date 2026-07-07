package device

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	qmimanager "github.com/boa-z/quectel-qmi-go/pkg/manager"
)

// QMIDevice 表示通过 sysfs 静态枚举到的 QMI 设备拓扑信息。
// 这里只包含静态发现结果，不包含任何主动探测出来的 IMEI 等动态信息。
type QMIDevice struct {
	ControlPath  string
	NetInterface string

	USBPath   string
	VendorID  uint16
	ProductID uint16

	DriverName string

	ATPorts      []string
	ATPort       string
	ATPortBackup string

	AudioDevice  string
	AudioCardNum int
}

// ToQMIManagerDevice 将静态发现结果转换为 quectel-qmi-go 的注入式设备描述。
func (m QMIDevice) ToQMIManagerDevice() qmimanager.ModemDevice {
	return qmimanager.ModemDevice{
		ControlPath:  m.ControlPath,
		NetInterface: m.NetInterface,
		USBPath:      m.USBPath,
		VendorID:     m.VendorID,
		ProductID:    m.ProductID,
		DriverName:   m.DriverName,
		ATPorts:      append([]string(nil), m.ATPorts...),
		ATPort:       m.ATPort,
		ATPortBackup: m.ATPortBackup,
		AudioDevice:  m.AudioDevice,
		AudioCardNum: m.AudioCardNum,
	}
}

// DiscoverQMIDevices 发现可用于 QMI 控制的调制解调器。
// 该入口要求结果最终必须具备 control path，适合正常业务接管流程。
func DiscoverQMIDevices() ([]QMIDevice, error) {
	return discoverQMIDevices(true)
}

// DiscoverAllQMIDevices 发现所有可识别的调制解调器（包含缺少 control path 的设备）。
// 主要用于调试或兼容场景；QMI 设备仍需满足 qmi_wwan + cdc-wdm 能力门。
func DiscoverAllQMIDevices() ([]QMIDevice, error) {
	return discoverQMIDevices(false)
}

// discoverQMIDevices 并发扫描 /sys/bus/usb/devices 下的 USB 设备。
// 这里严格保持“纯静态发现”边界：只读取 sysfs，不打开 /dev，也不发送任何 QMI/AT 请求。
func discoverQMIDevices(requireControlPath bool) ([]QMIDevice, error) {
	var devices []QMIDevice

	usbDevices, err := os.ReadDir("/sys/bus/usb/devices")
	if err != nil {
		if wwanDevices, wwanErr := discoverWWANQMIDevices(); wwanErr == nil && len(wwanDevices) > 0 {
			return wwanDevices, nil
		}
		return nil, fmt.Errorf("读取 USB 设备失败: %w", err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, entry := range usbDevices {
		if strings.HasPrefix(entry.Name(), "usb") {
			continue
		}

		wg.Add(1)
		go func(e os.DirEntry) {
			defer wg.Done()

			path := filepath.Join("/sys/bus/usb/devices", e.Name())
			type result struct {
				val *QMIDevice
				err error
			}
			done := make(chan result, 1)

			go func() {
				md, err := discoverQMIDeviceFromSysFS(path)
				done <- result{md, err}
			}()

			select {
			case res := <-done:
				if res.err == nil && res.val != nil {
					// 默认发现流程要求设备具备 QMI 控制口；必要时调用方也可以放宽这个约束。
					if requireControlPath && strings.TrimSpace(res.val.ControlPath) == "" {
						return
					}
					mu.Lock()
					devices = append(devices, *res.val)
					mu.Unlock()
				}
			case <-time.After(5 * time.Second):
			}
		}(entry)
	}

	wg.Wait()

	if wwanDevices, err := discoverWWANQMIDevices(); err == nil && len(wwanDevices) > 0 {
		devices = mergeQMIDeviceLists(devices, wwanDevices)
	}

	if len(devices) == 0 {
		return nil, fmt.Errorf("未发现调制解调器")
	}

	return devices, nil
}

// discoverQMIDeviceFromSysFS 基于单个 USB sysfs 根目录解析一台设备的静态拓扑。
// 这里只返回“看得见的结构信息”，不会主动读取 IMEI，也不会验证 AT 口是否真的可用。
func discoverQMIDeviceFromSysFS(usbPath string) (*QMIDevice, error) {
	scanUSBPath := resolveUSBPath(usbPath)

	vid := readHexFile(filepath.Join(scanUSBPath, "idVendor"))
	pid := readHexFile(filepath.Join(scanUSBPath, "idProduct"))
	capability, ok := detectQMIUSBCapability(scanUSBPath)
	if !ok {
		return nil, fmt.Errorf("不是 QMI capable 设备")
	}

	md := &QMIDevice{
		USBPath:      usbPath,
		VendorID:     vid,
		ProductID:    pid,
		NetInterface: capability.NetInterface,
		DriverName:   capability.DriverName,
		ControlPath:  capability.ControlPath,
	}

	// atIntf 只是一个静态“主候选口”提示，来自常见 Quectel 机型的 interface 经验值；
	// 真正哪个 AT 口可用，仍由上层在本设备 ATPorts 范围内继续探测确认。
	atIntf := -1
	if vid == 0x2c7c {
		switch pid {
		case 0x0901, 0x0902, 0x8101:
			atIntf = 2
		case 0x0900:
			atIntf = 4
		case 0x6026, 0x6005, 0x6002, 0x6001:
			atIntf = 3
		case 0x6007:
			atIntf = 3
		default:
			atIntf = 2
		}
	} else if vid == 0x05c6 {
		atIntf = 2
	} else if isSIMCOMUSBID(vid, pid) {
		atIntf = 2
	}

	// discovery 阶段保留该设备下的全部 AT 候选口，避免过早做主观裁剪。
	md.ATPorts = findATPorts(scanUSBPath)

	staticPrimary := ""
	if atIntf != -1 {
		atIfPath := filepath.Join(scanUSBPath, fmt.Sprintf("%s:1.%d", filepath.Base(scanUSBPath), atIntf))
		primary, err := findTTYInInterface(atIfPath)
		if err == nil && primary != "" {
			staticPrimary = primary
		}
	}
	md.ATPort, md.ATPortBackup = chooseStaticATPorts(md.ATPorts, staticPrimary)

	md.AudioDevice, md.AudioCardNum = findAudioDevice(scanUSBPath)

	return md, nil
}

func isSIMCOMUSBID(vid uint16, pid uint16) bool {
	if vid != 0x1e0e {
		return false
	}
	switch pid {
	case 0x9000, 0x9001, 0x9002, 0x9003, 0x9004, 0x9005, 0x9006, 0x9007,
		0x9011, 0x9016, 0x9018, 0x9019, 0x901a, 0x901b,
		0x9020, 0x9021, 0x9022, 0x9023, 0x9024, 0x9025, 0x9026, 0x9027,
		0x9028, 0x9029, 0x902a, 0x902b:
		return true
	default:
		return false
	}
}

func discoverWWANQMIDevices() ([]QMIDevice, error) {
	classDevices, classErr := discoverWWANQMIDevicesFromClass("/sys/class/wwan")
	devDevices, devErr := discoverWWANQMIDevicesFromDev("/dev")
	merged := mergeQMIDeviceLists(classDevices, devDevices)
	if len(merged) > 0 {
		return merged, nil
	}
	if classErr != nil {
		return nil, classErr
	}
	return nil, devErr
}

func discoverWWANQMIDevicesFromClass(wwanClassPath string) ([]QMIDevice, error) {
	entries, err := os.ReadDir(wwanClassPath)
	if err != nil {
		return nil, fmt.Errorf("读取 WWAN 设备失败: %w", err)
	}

	type group struct {
		iface        string
		controlPaths []string
		atPorts      []string
	}

	groups := make(map[string]*group)
	for _, entry := range entries {
		iface, kind := splitWWANPortName(entry.Name())
		if iface == "" {
			continue
		}
		g := groups[iface]
		if g == nil {
			g = &group{iface: iface}
			groups[iface] = g
		}

		devPath := filepath.Join("/dev", entry.Name())
		switch kind {
		case "qmi":
			g.controlPaths = append(g.controlPaths, devPath)
		case "at":
			g.atPorts = append(g.atPorts, devPath)
		}
	}

	devices := make([]QMIDevice, 0, len(groups))
	for _, g := range groups {
		controls := dedupSortedNonEmpty(g.controlPaths)
		if len(controls) == 0 {
			continue
		}
		atPorts := normalizeATPorts(g.atPorts)
		primary, backup := chooseStaticATPorts(atPorts, "")
		devices = append(devices, QMIDevice{
			ControlPath:  controls[0],
			NetInterface: g.iface,
			USBPath:      filepath.Join(wwanClassPath, g.iface),
			DriverName:   "wwan_qmi",
			ATPorts:      atPorts,
			ATPort:       primary,
			ATPortBackup: backup,
		})
	}
	sort.SliceStable(devices, func(i, j int) bool {
		return devices[i].NetInterface < devices[j].NetInterface
	})

	if len(devices) == 0 {
		return nil, fmt.Errorf("未发现 WWAN QMI 设备")
	}
	return devices, nil
}

func discoverWWANQMIDevicesFromDev(devDir string) ([]QMIDevice, error) {
	entries, err := os.ReadDir(devDir)
	if err != nil {
		return nil, fmt.Errorf("读取 WWAN 设备节点失败: %w", err)
	}

	type group struct {
		iface        string
		controlPaths []string
		atPorts      []string
	}

	groups := make(map[string]*group)
	for _, entry := range entries {
		iface, kind := splitWWANPortName(entry.Name())
		if iface == "" {
			continue
		}
		g := groups[iface]
		if g == nil {
			g = &group{iface: iface}
			groups[iface] = g
		}

		devPath := filepath.Join(devDir, entry.Name())
		switch kind {
		case "qmi":
			g.controlPaths = append(g.controlPaths, devPath)
		case "at":
			g.atPorts = append(g.atPorts, devPath)
		}
	}

	devices := make([]QMIDevice, 0, len(groups))
	for _, g := range groups {
		controls := dedupSortedNonEmpty(g.controlPaths)
		if len(controls) == 0 {
			continue
		}
		atPorts := normalizeATPorts(g.atPorts)
		primary, backup := chooseStaticATPorts(atPorts, "")
		devices = append(devices, QMIDevice{
			ControlPath:  controls[0],
			NetInterface: g.iface,
			USBPath:      filepath.Join("/sys/class/wwan", g.iface),
			DriverName:   "wwan_qmi",
			ATPorts:      atPorts,
			ATPort:       primary,
			ATPortBackup: backup,
		})
	}
	sort.SliceStable(devices, func(i, j int) bool {
		return devices[i].NetInterface < devices[j].NetInterface
	})

	if len(devices) == 0 {
		return nil, fmt.Errorf("未发现 WWAN QMI 设备节点")
	}
	return devices, nil
}

func splitWWANPortName(name string) (iface, kind string) {
	name = strings.TrimSpace(name)
	if !strings.HasPrefix(name, "wwan") {
		return "", ""
	}
	for _, marker := range []string{"qmi", "at"} {
		idx := strings.Index(name, marker)
		if idx <= len("wwan") {
			continue
		}
		prefix := name[:idx]
		if !isBareWWANInterfaceName(prefix) {
			continue
		}
		return prefix, marker
	}
	if isBareWWANInterfaceName(name) {
		return name, "parent"
	}
	return "", ""
}

func isBareWWANInterfaceName(name string) bool {
	if !strings.HasPrefix(name, "wwan") {
		return false
	}
	suffix := strings.TrimPrefix(name, "wwan")
	if suffix == "" {
		return false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func mergeQMIDeviceLists(lists ...[]QMIDevice) []QMIDevice {
	out := make([]QMIDevice, 0)
	seen := make(map[string]struct{})

	for _, list := range lists {
		for _, dev := range list {
			keys := qmiDeviceDedupKeys(dev)
			duplicate := false
			for _, key := range keys {
				if _, ok := seen[key]; ok {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			for _, key := range keys {
				seen[key] = struct{}{}
			}
			out = append(out, dev)
		}
	}
	return out
}

func qmiDeviceDedupKeys(dev QMIDevice) []string {
	keys := make([]string, 0, 3)
	if v := strings.TrimSpace(dev.ControlPath); v != "" {
		keys = append(keys, "control:"+v)
	}
	if v := strings.TrimSpace(dev.USBPath); v != "" {
		keys = append(keys, "usb:"+v)
	}
	if v := strings.TrimSpace(dev.NetInterface); v != "" {
		keys = append(keys, "iface:"+v)
	}
	return keys
}

type qmiUSBCapability struct {
	InterfacePath string
	NetInterface  string
	DriverName    string
	ControlPath   string
}

func detectQMIUSBCapability(scanUSBPath string) (qmiUSBCapability, bool) {
	usbName := filepath.Base(scanUSBPath)
	pattern := filepath.Join(scanUSBPath, usbName+":1.*")
	ifaces, err := filepath.Glob(pattern)
	if err != nil {
		return qmiUSBCapability{}, false
	}
	sortUSBInterfacePaths(ifaces)

	for _, ifPath := range ifaces {
		driver := determineDriver(ifPath)
		if driver != "qmi_wwan" {
			continue
		}

		netInterface := firstNetInterfaceInUSBInterface(ifPath)
		if netInterface == "" {
			continue
		}

		controlPath := findCDCWDM(ifPath)
		if controlPath == "" {
			controlPath = findCDCWDMInUSB(scanUSBPath)
		}
		if controlPath == "" {
			continue
		}

		return qmiUSBCapability{
			InterfacePath: ifPath,
			NetInterface:  netInterface,
			DriverName:    driver,
			ControlPath:   controlPath,
		}, true
	}

	return qmiUSBCapability{}, false
}

func detectMBIMUSBCapability(scanUSBPath string) (qmiUSBCapability, bool) {
	usbName := filepath.Base(scanUSBPath)
	pattern := filepath.Join(scanUSBPath, usbName+":1.*")
	ifaces, err := filepath.Glob(pattern)
	if err != nil {
		return qmiUSBCapability{}, false
	}
	sortUSBInterfacePaths(ifaces)

	for _, ifPath := range ifaces {
		driver := determineDriver(ifPath)
		if driver != "cdc_mbim" {
			continue
		}
		netInterface := firstNetInterfaceInUSBInterface(ifPath)
		if netInterface == "" {
			continue
		}
		controlPath := findCDCWDM(ifPath)
		if controlPath == "" {
			controlPath = findCDCWDMInUSB(scanUSBPath)
		}
		if controlPath == "" {
			continue
		}
		return qmiUSBCapability{
			InterfacePath: ifPath,
			NetInterface:  netInterface,
			DriverName:    driver,
			ControlPath:   controlPath,
		}, true
	}
	return qmiUSBCapability{}, false
}

func firstNetInterfaceInUSBInterface(ifPath string) string {
	entries, err := os.ReadDir(filepath.Join(ifPath, "net"))
	if err != nil || len(entries) == 0 {
		return ""
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names[0]
}

func sortUSBInterfacePaths(paths []string) {
	sort.SliceStable(paths, func(i, j int) bool {
		ni, iok := usbInterfaceNumber(paths[i])
		nj, jok := usbInterfaceNumber(paths[j])
		if iok && jok && ni != nj {
			return ni < nj
		}
		if iok != jok {
			return iok
		}
		return paths[i] < paths[j]
	})
}

func usbInterfaceNumber(path string) (int, bool) {
	base := filepath.Base(strings.TrimSpace(path))
	idx := strings.LastIndex(base, ":1.")
	if idx == -1 {
		return 0, false
	}
	n, err := strconv.Atoi(base[idx+len(":1."):])
	if err != nil {
		return 0, false
	}
	return n, true
}

// findCDCWDM 在单个 interface 目录附近搜索 cdc-wdm 控制口。
// 这里兼容 usbmisc/usb 两种常见 sysfs 布局。
func findCDCWDM(devicePath string) string {
	for _, subDir := range []string{"usbmisc", "usb"} {
		miscPath := filepath.Join(devicePath, subDir)
		entries, err := os.ReadDir(miscPath)
		if err == nil {
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), "cdc-wdm") {
					return filepath.Join("/dev", e.Name())
				}
			}
		}
	}
	return ""
}

// findCDCWDMInUSB 在整台 USB 设备树内回退搜索 cdc-wdm 控制口。
// 用于接口局部目录未命中时的兜底，减少不同内核拓扑带来的漏检。
func findCDCWDMInUSB(usbPath string) string {
	var result string

	filepath.Walk(resolveUSBPath(usbPath), func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if info.Name() != "usbmisc" && info.Name() != "usb" {
			return nil
		}
		if !info.IsDir() && (info.Mode()&os.ModeSymlink) == 0 {
			return nil
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "cdc-wdm") {
				result = filepath.Join("/dev", e.Name())
				return filepath.SkipAll
			}
		}
		return nil
	})

	return result
}

// resolveUSBPath 尽量把 USB sysfs 路径解析到真实目录，统一后续扫描入口。
func resolveUSBPath(usbPath string) string {
	p := strings.TrimSpace(usbPath)
	if p == "" {
		return p
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil || strings.TrimSpace(resolved) == "" {
		return p
	}
	return resolved
}

// findATPorts 收集某台 USB 设备下全部串口候选。
// 同时兼容 `.../ttyUSB*`、`.../ttyACM*` 和 `.../tty/<name>` 常见目录布局。
func findATPorts(usbPath string) []string {
	var ports []string

	for _, ttyPattern := range []string{"ttyUSB*", "ttyACM*"} {
		patterns := []string{
			filepath.Join(usbPath, "*", ttyPattern),
			filepath.Join(usbPath, "*", "tty", ttyPattern),
		}

		for _, pattern := range patterns {
			matches, err := filepath.Glob(pattern)
			if err != nil {
				continue
			}
			for _, match := range matches {
				ttyName := filepath.Base(match)
				ports = append(ports, filepath.Join("/dev", ttyName))
			}
		}
	}

	return sortATPortCandidates(ports)
}

// normalizeATPorts 对候选端口做去空、去重和稳定排序，确保上层处理顺序可预测。
func normalizeATPorts(ports []string) []string {
	return sortATPortCandidates(ports)
}

// chooseStaticATPorts 在全量候选口里选出静态主口和备用口。
// hintedPrimary 只是优先级提示，如果不属于本设备端口集合则会被忽略。
func chooseStaticATPorts(atPorts []string, hintedPrimary string) (primary, backup string) {
	ports := normalizeATPorts(atPorts)
	if len(ports) == 0 {
		return "", ""
	}

	ordered := ports
	hint := strings.TrimSpace(hintedPrimary)
	if hint != "" {
		for i, port := range ports {
			if port != hint {
				continue
			}
			ordered = make([]string, 0, len(ports))
			ordered = append(ordered, hint)
			ordered = append(ordered, ports[:i]...)
			ordered = append(ordered, ports[i+1:]...)
			break
		}
	}

	primary = ordered[0]
	if len(ordered) > 1 {
		backup = ordered[1]
	}
	return primary, backup
}

// determineDriver 读取 interface 绑定的驱动名，例如 qmi_wwan / cdc_mbim / cdc_ether。
func determineDriver(devicePath string) string {
	driverLink := filepath.Join(devicePath, "driver")
	target, err := os.Readlink(driverLink)
	if err != nil {
		return ""
	}
	return filepath.Base(target)
}

// readHexFile 读取十六进制 sysfs 字段，如 idVendor / idProduct。
func readHexFile(path string) uint16 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	val, err := strconv.ParseUint(strings.TrimSpace(string(data)), 16, 16)
	if err != nil {
		return 0
	}
	return uint16(val)
}

// findTTYInInterface 在指定 USB interface 目录中寻找 tty 设备节点。
// 该结果仅用于静态主候选提示，不表示该端口已经被验证可用。
func findTTYInInterface(ifPath string) (string, error) {
	ttyDir := filepath.Join(ifPath, "tty")
	entries, err := os.ReadDir(ttyDir)
	if err == nil {
		ports := make([]string, 0, len(entries))
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "ttyUSB") || strings.HasPrefix(e.Name(), "ttyACM") {
				ports = append(ports, filepath.Join("/dev", e.Name()))
			}
		}
		ports = sortATPortCandidates(ports)
		if len(ports) > 0 {
			return ports[0], nil
		}
	}

	for _, ttyPattern := range []string{"ttyUSB*", "ttyACM*"} {
		matches, _ := filepath.Glob(filepath.Join(ifPath, ttyPattern))
		sortTTYPaths(matches)
		if len(matches) > 0 {
			return filepath.Join("/dev", filepath.Base(matches[0])), nil
		}

		matches, _ = filepath.Glob(filepath.Join(ifPath, "tty", ttyPattern))
		sortTTYPaths(matches)
		if len(matches) > 0 {
			return filepath.Join("/dev", filepath.Base(matches[0])), nil
		}
	}

	return "", fmt.Errorf("未找到 tty")
}

func sortTTYPaths(paths []string) {
	sort.SliceStable(paths, func(i, j int) bool {
		pi := atPortPriority(paths[i])
		pj := atPortPriority(paths[j])
		if pi != pj {
			return pi < pj
		}
		return paths[i] < paths[j]
	})
}

// findAudioDevice 在同一 USB 复合设备下关联 ALSA 声卡信息。
// 这样上层就能把 modem 控制面和同设备的 USB Audio 能力关联起来。
func findAudioDevice(usbPath string) (string, int) {
	usbName := filepath.Base(usbPath)
	pattern := filepath.Join(usbPath, usbName+":1.*", "sound", "card*")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return "", -1
	}

	cardDir := filepath.Base(matches[0])
	if !strings.HasPrefix(cardDir, "card") {
		return "", -1
	}
	cardNumStr := strings.TrimPrefix(cardDir, "card")
	cardNum, err := strconv.Atoi(cardNumStr)
	if err != nil {
		return "", -1
	}

	alsaDev := fmt.Sprintf("hw:%d,0", cardNum)
	return alsaDev, cardNum
}

// String 返回便于日志输出的简短设备描述。
func (m QMIDevice) String() string {
	s := fmt.Sprintf("%s (%s) [%04x:%04x] driver=%s AT=%s Backup=%s",
		m.ControlPath, m.NetInterface, m.VendorID, m.ProductID, m.DriverName, m.ATPort, m.ATPortBackup)
	if m.AudioDevice != "" {
		s += fmt.Sprintf(" Audio=%s", m.AudioDevice)
	}
	return s
}

package device

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNormalizeATPorts(t *testing.T) {
	got := normalizeATPorts([]string{
		" /dev/ttyUSB7 ",
		"/dev/ttyUSB6",
		"",
		"/dev/ttyUSB7",
		"/dev/ttyUSB4",
	})

	want := []string{"/dev/ttyUSB4", "/dev/ttyUSB6", "/dev/ttyUSB7"}
	if len(got) != len(want) {
		t.Fatalf("len=%d want=%d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q want=%q (all=%v)", i, got[i], want[i], got)
		}
	}
}

func TestChooseStaticATPortsPrefersHintWithinDevicePorts(t *testing.T) {
	primary, backup := chooseStaticATPorts(
		[]string{"/dev/ttyUSB6", "/dev/ttyUSB7", "/dev/ttyUSB4"},
		"/dev/ttyUSB7",
	)

	if primary != "/dev/ttyUSB7" {
		t.Fatalf("primary=%q want=%q", primary, "/dev/ttyUSB7")
	}
	if backup != "/dev/ttyUSB4" {
		t.Fatalf("backup=%q want=%q", backup, "/dev/ttyUSB4")
	}
}

func TestChooseStaticATPortsIgnoresHintOutsideDevicePorts(t *testing.T) {
	primary, backup := chooseStaticATPorts(
		[]string{"/dev/ttyUSB6", "/dev/ttyUSB7"},
		"/dev/ttyUSB4",
	)

	if primary != "/dev/ttyUSB6" {
		t.Fatalf("primary=%q want=%q", primary, "/dev/ttyUSB6")
	}
	if backup != "/dev/ttyUSB7" {
		t.Fatalf("backup=%q want=%q", backup, "/dev/ttyUSB7")
	}
}

func TestChooseStaticATPortsPrioritizesTTYUSBOverTTYACMWithoutHint(t *testing.T) {
	primary, backup := chooseStaticATPorts(
		[]string{"/dev/ttyACM0", "/dev/ttyUSB6"},
		"",
	)

	if primary != "/dev/ttyUSB6" {
		t.Fatalf("primary=%q want %q", primary, "/dev/ttyUSB6")
	}
	if backup != "/dev/ttyACM0" {
		t.Fatalf("backup=%q want %q", backup, "/dev/ttyACM0")
	}
}

func TestFindATPortsCollectsTTYUSBandTTYACMLayouts(t *testing.T) {
	usbPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(usbPath, "1-2:1.2", "ttyUSB6"), 0o755); err != nil {
		t.Fatalf("mkdir direct ttyUSB layout: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(usbPath, "1-2:1.3", "tty", "ttyUSB7"), 0o755); err != nil {
		t.Fatalf("mkdir nested ttyUSB layout: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(usbPath, "1-2:1.4", "ttyACM0"), 0o755); err != nil {
		t.Fatalf("mkdir direct ttyACM layout: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(usbPath, "1-2:1.5", "tty", "ttyACM1"), 0o755); err != nil {
		t.Fatalf("mkdir nested ttyACM layout: %v", err)
	}

	got := findATPorts(usbPath)
	want := []string{"/dev/ttyUSB6", "/dev/ttyUSB7", "/dev/ttyACM0", "/dev/ttyACM1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findATPorts()=%v want=%v", got, want)
	}
}

func TestDiscoverQMIDeviceFromSysFSStaticTopologyOnly(t *testing.T) {
	usbPath := t.TempDir()
	usbName := filepath.Base(usbPath)

	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(usbPath, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	write("idVendor", "2c7c\n")
	write("idProduct", "0125\n")
	write("bNumInterfaces", "5\n")

	ifacePath := filepath.Join(usbPath, usbName+":1.4")
	if err := os.MkdirAll(filepath.Join(ifacePath, "net", "wwan9"), 0o755); err != nil {
		t.Fatalf("mkdir net iface: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(ifacePath, "usbmisc", "cdc-wdm9"), 0o755); err != nil {
		t.Fatalf("mkdir cdc-wdm tree: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(usbPath, usbName+":1.2", "tty", "ttyUSB6"), 0o755); err != nil {
		t.Fatalf("mkdir at port tree: %v", err)
	}
	if err := os.Symlink("/tmp/qmi_wwan", filepath.Join(ifacePath, "driver")); err != nil {
		t.Fatalf("symlink driver: %v", err)
	}

	got, err := discoverQMIDeviceFromSysFS(usbPath)
	if err != nil {
		t.Fatalf("discoverQMIDeviceFromSysFS() error = %v", err)
	}
	if got == nil {
		t.Fatal("discoverQMIDeviceFromSysFS() returned nil")
	}
	if got.ControlPath != "/dev/cdc-wdm9" {
		t.Fatalf("ControlPath=%q want %q", got.ControlPath, "/dev/cdc-wdm9")
	}
	if got.ATPort != "/dev/ttyUSB6" {
		t.Fatalf("ATPort=%q want %q", got.ATPort, "/dev/ttyUSB6")
	}
}

func TestDiscoverQMIDeviceFromSysFSSIMCOMPrefersInterfaceTwoATPort(t *testing.T) {
	usbPath := t.TempDir()
	usbName := filepath.Base(usbPath)

	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(usbPath, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	write("idVendor", "1e0e\n")
	write("idProduct", "9001\n")

	qmiPath := filepath.Join(usbPath, usbName+":1.5")
	if err := os.MkdirAll(filepath.Join(qmiPath, "net", "wwan0"), 0o755); err != nil {
		t.Fatalf("mkdir net iface: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(qmiPath, "usbmisc", "cdc-wdm0"), 0o755); err != nil {
		t.Fatalf("mkdir cdc-wdm tree: %v", err)
	}
	if err := os.Symlink("/tmp/qmi_wwan", filepath.Join(qmiPath, "driver")); err != nil {
		t.Fatalf("symlink driver: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(usbPath, usbName+":1.1", "tty", "ttyUSB1"), 0o755); err != nil {
		t.Fatalf("mkdir gps port tree: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(usbPath, usbName+":1.2", "tty", "ttyUSB2"), 0o755); err != nil {
		t.Fatalf("mkdir at port tree: %v", err)
	}

	got, err := discoverQMIDeviceFromSysFS(usbPath)
	if err != nil {
		t.Fatalf("discoverQMIDeviceFromSysFS() error = %v", err)
	}
	if got.VendorID != 0x1e0e || got.ProductID != 0x9001 {
		t.Fatalf("ids=%04x:%04x want 1e0e:9001", got.VendorID, got.ProductID)
	}
	if got.ATPort != "/dev/ttyUSB2" {
		t.Fatalf("ATPort=%q want /dev/ttyUSB2", got.ATPort)
	}
}

func TestDiscoverQMIDeviceFromSysFSAcceptsSierraQMIByCapability(t *testing.T) {
	usbPath := t.TempDir()
	usbName := filepath.Base(usbPath)

	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(usbPath, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	write("idVendor", "1199\n")
	write("idProduct", "9077\n")
	write("bNumInterfaces", "5\n")

	ifacePath := filepath.Join(usbPath, usbName+":1.8")
	if err := os.MkdirAll(filepath.Join(ifacePath, "net", "wwan5"), 0o755); err != nil {
		t.Fatalf("mkdir net iface: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(ifacePath, "usbmisc", "cdc-wdm5"), 0o755); err != nil {
		t.Fatalf("mkdir cdc-wdm tree: %v", err)
	}
	if err := os.Symlink("/tmp/qmi_wwan", filepath.Join(ifacePath, "driver")); err != nil {
		t.Fatalf("symlink driver: %v", err)
	}

	got, err := discoverQMIDeviceFromSysFS(usbPath)
	if err != nil {
		t.Fatalf("discoverQMIDeviceFromSysFS() error = %v", err)
	}
	if got == nil {
		t.Fatal("discoverQMIDeviceFromSysFS() returned nil")
	}
	if got.VendorID != 0x1199 || got.ProductID != 0x9077 {
		t.Fatalf("ids=%04x:%04x want 1199:9077", got.VendorID, got.ProductID)
	}
	if got.DriverName != "qmi_wwan" {
		t.Fatalf("DriverName=%q want qmi_wwan", got.DriverName)
	}
	if got.NetInterface != "wwan5" {
		t.Fatalf("NetInterface=%q want wwan5", got.NetInterface)
	}
	if got.ControlPath != "/dev/cdc-wdm5" {
		t.Fatalf("ControlPath=%q want /dev/cdc-wdm5", got.ControlPath)
	}
	if got.ATPort != "" || got.ATPortBackup != "" || len(got.ATPorts) != 0 {
		t.Fatalf("expected pure QMI without AT ports, got ATPort=%q backup=%q ports=%v", got.ATPort, got.ATPortBackup, got.ATPorts)
	}
}

func TestDiscoverQMIDeviceFromSysFSPrefersLowerNumericQMIInterface(t *testing.T) {
	usbPath := t.TempDir()
	usbName := filepath.Base(usbPath)

	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(usbPath, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	write("idVendor", "1199\n")
	write("idProduct", "9077\n")

	for _, tc := range []struct {
		idx     string
		iface   string
		control string
	}{
		{idx: "10", iface: "wwan10", control: "cdc-wdm10"},
		{idx: "2", iface: "wwan2", control: "cdc-wdm2"},
	} {
		ifacePath := filepath.Join(usbPath, usbName+":1."+tc.idx)
		if err := os.MkdirAll(filepath.Join(ifacePath, "net", tc.iface), 0o755); err != nil {
			t.Fatalf("mkdir net iface %s: %v", tc.iface, err)
		}
		if err := os.MkdirAll(filepath.Join(ifacePath, "usbmisc", tc.control), 0o755); err != nil {
			t.Fatalf("mkdir cdc-wdm tree %s: %v", tc.control, err)
		}
		if err := os.Symlink("/tmp/qmi_wwan", filepath.Join(ifacePath, "driver")); err != nil {
			t.Fatalf("symlink driver %s: %v", tc.idx, err)
		}
	}

	got, err := discoverQMIDeviceFromSysFS(usbPath)
	if err != nil {
		t.Fatalf("discoverQMIDeviceFromSysFS() error = %v", err)
	}
	if got.NetInterface != "wwan2" {
		t.Fatalf("NetInterface=%q want wwan2", got.NetInterface)
	}
	if got.ControlPath != "/dev/cdc-wdm2" {
		t.Fatalf("ControlPath=%q want /dev/cdc-wdm2", got.ControlPath)
	}
}

func TestDiscoverQMIDeviceFromSysFSFindsSymlinkUSBMiscOutsideQMIInterface(t *testing.T) {
	usbPath := t.TempDir()
	usbName := filepath.Base(usbPath)

	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(usbPath, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	write("idVendor", "1199\n")
	write("idProduct", "9077\n")

	qmiIfacePath := filepath.Join(usbPath, usbName+":1.8")
	if err := os.MkdirAll(filepath.Join(qmiIfacePath, "net", "wwan5"), 0o755); err != nil {
		t.Fatalf("mkdir net iface: %v", err)
	}
	if err := os.Symlink("/tmp/qmi_wwan", filepath.Join(qmiIfacePath, "driver")); err != nil {
		t.Fatalf("symlink driver: %v", err)
	}

	otherIfacePath := filepath.Join(usbPath, usbName+":1.9")
	realUSBMisc := filepath.Join(otherIfacePath, "usbmisc-real")
	if err := os.MkdirAll(realUSBMisc, 0o755); err != nil {
		t.Fatalf("mkdir real usbmisc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(realUSBMisc, "cdc-wdm5"), []byte{}, 0o644); err != nil {
		t.Fatalf("create cdc-wdm: %v", err)
	}
	if err := os.Symlink("usbmisc-real", filepath.Join(otherIfacePath, "usbmisc")); err != nil {
		t.Fatalf("symlink usbmisc: %v", err)
	}

	got, err := discoverQMIDeviceFromSysFS(usbPath)
	if err != nil {
		t.Fatalf("discoverQMIDeviceFromSysFS() error = %v", err)
	}
	if got.ControlPath != "/dev/cdc-wdm5" {
		t.Fatalf("ControlPath=%q want /dev/cdc-wdm5", got.ControlPath)
	}
}

func TestDiscoverQMIDeviceFromSysFSRejectsQMIWithoutControlPath(t *testing.T) {
	usbPath := t.TempDir()
	usbName := filepath.Base(usbPath)

	if err := os.WriteFile(filepath.Join(usbPath, "idVendor"), []byte("1199\n"), 0o644); err != nil {
		t.Fatalf("write idVendor: %v", err)
	}
	if err := os.WriteFile(filepath.Join(usbPath, "idProduct"), []byte("9077\n"), 0o644); err != nil {
		t.Fatalf("write idProduct: %v", err)
	}
	if err := os.WriteFile(filepath.Join(usbPath, "bNumInterfaces"), []byte("5\n"), 0o644); err != nil {
		t.Fatalf("write bNumInterfaces: %v", err)
	}

	ifacePath := filepath.Join(usbPath, usbName+":1.8")
	if err := os.MkdirAll(filepath.Join(ifacePath, "net", "wwan5"), 0o755); err != nil {
		t.Fatalf("mkdir net iface: %v", err)
	}
	if err := os.Symlink("/tmp/qmi_wwan", filepath.Join(ifacePath, "driver")); err != nil {
		t.Fatalf("symlink driver: %v", err)
	}

	got, err := discoverQMIDeviceFromSysFS(usbPath)
	if err == nil || got != nil {
		t.Fatalf("expected rejection without cdc-wdm, got dev=%+v err=%v", got, err)
	}
}

func TestDiscoverQMIDeviceFromSysFSRejectsUnknownVendorNonQMI(t *testing.T) {
	usbPath := t.TempDir()
	usbName := filepath.Base(usbPath)

	if err := os.WriteFile(filepath.Join(usbPath, "idVendor"), []byte("1199\n"), 0o644); err != nil {
		t.Fatalf("write idVendor: %v", err)
	}
	if err := os.WriteFile(filepath.Join(usbPath, "idProduct"), []byte("9077\n"), 0o644); err != nil {
		t.Fatalf("write idProduct: %v", err)
	}
	if err := os.WriteFile(filepath.Join(usbPath, "bNumInterfaces"), []byte("5\n"), 0o644); err != nil {
		t.Fatalf("write bNumInterfaces: %v", err)
	}

	ifacePath := filepath.Join(usbPath, usbName+":1.8")
	if err := os.MkdirAll(filepath.Join(ifacePath, "net", "wwan5"), 0o755); err != nil {
		t.Fatalf("mkdir net iface: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(ifacePath, "usbmisc", "cdc-wdm5"), 0o755); err != nil {
		t.Fatalf("mkdir cdc-wdm tree: %v", err)
	}
	if err := os.Symlink("/tmp/cdc_mbim", filepath.Join(ifacePath, "driver")); err != nil {
		t.Fatalf("symlink driver: %v", err)
	}

	got, err := discoverQMIDeviceFromSysFS(usbPath)
	if err == nil || got != nil {
		t.Fatalf("expected non-QMI rejection, got dev=%+v err=%v", got, err)
	}
}

func TestDiscoverWWANQMIDevicesFromClassQualcomm410Topology(t *testing.T) {
	wwanClass := t.TempDir()

	for _, name := range []string{"wwan0", "wwan0at0", "wwan0at1", "wwan0qmi0"} {
		if err := os.MkdirAll(filepath.Join(wwanClass, name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}

	got, err := discoverWWANQMIDevicesFromClass(wwanClass)
	if err != nil {
		t.Fatalf("discoverWWANQMIDevicesFromClass() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d want 1 (%+v)", len(got), got)
	}

	dev := got[0]
	if dev.ControlPath != "/dev/wwan0qmi0" {
		t.Fatalf("ControlPath=%q want %q", dev.ControlPath, "/dev/wwan0qmi0")
	}
	if dev.NetInterface != "wwan0" {
		t.Fatalf("NetInterface=%q want %q", dev.NetInterface, "wwan0")
	}
	if dev.USBPath != filepath.Join(wwanClass, "wwan0") {
		t.Fatalf("USBPath=%q want %q", dev.USBPath, filepath.Join(wwanClass, "wwan0"))
	}
	if dev.DriverName != "wwan_qmi" {
		t.Fatalf("DriverName=%q want %q", dev.DriverName, "wwan_qmi")
	}
	wantATPorts := []string{"/dev/wwan0at0", "/dev/wwan0at1"}
	if !reflect.DeepEqual(dev.ATPorts, wantATPorts) {
		t.Fatalf("ATPorts=%v want %v", dev.ATPorts, wantATPorts)
	}
	if dev.ATPort != "/dev/wwan0at0" {
		t.Fatalf("ATPort=%q want %q", dev.ATPort, "/dev/wwan0at0")
	}
}

func TestDiscoverWWANQMIDevicesFromDevFallback(t *testing.T) {
	devDir := t.TempDir()
	for _, name := range []string{"wwan0at0", "wwan0at1", "wwan0qmi0"} {
		if err := os.WriteFile(filepath.Join(devDir, name), []byte{}, 0o644); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}

	got, err := discoverWWANQMIDevicesFromDev(devDir)
	if err != nil {
		t.Fatalf("discoverWWANQMIDevicesFromDev() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d want 1 (%+v)", len(got), got)
	}
	dev := got[0]
	if dev.ControlPath != filepath.Join(devDir, "wwan0qmi0") {
		t.Fatalf("ControlPath=%q want %q", dev.ControlPath, filepath.Join(devDir, "wwan0qmi0"))
	}
	if dev.NetInterface != "wwan0" {
		t.Fatalf("NetInterface=%q want %q", dev.NetInterface, "wwan0")
	}
	wantATPorts := []string{filepath.Join(devDir, "wwan0at0"), filepath.Join(devDir, "wwan0at1")}
	if !reflect.DeepEqual(dev.ATPorts, wantATPorts) {
		t.Fatalf("ATPorts=%v want %v", dev.ATPorts, wantATPorts)
	}
}

func TestMergeQMIDeviceListsDedupsByControlPath(t *testing.T) {
	usb := []QMIDevice{
		{ControlPath: "/dev/wwan0qmi0", NetInterface: "wwan0", USBPath: "/sys/bus/usb/devices/1-1"},
	}
	wwan := []QMIDevice{
		{ControlPath: "/dev/wwan0qmi0", NetInterface: "wwan0", USBPath: "/sys/class/wwan/wwan0", ATPort: "/dev/wwan0at0"},
		{ControlPath: "/dev/wwan1qmi0", NetInterface: "wwan1", USBPath: "/sys/class/wwan/wwan1"},
	}

	got := mergeQMIDeviceLists(usb, wwan)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2 (%+v)", len(got), got)
	}
	if got[0].ControlPath != "/dev/wwan0qmi0" || got[0].ATPort != "" {
		t.Fatalf("expected original USB entry to win for duplicate control path, got %+v", got[0])
	}
	if got[1].ControlPath != "/dev/wwan1qmi0" {
		t.Fatalf("expected second WWAN entry retained, got %+v", got[1])
	}
}

package api

import (
	"strings"
	"testing"

	"github.com/zanescope/vohive/internal/config"
)

func TestValidateFreeDeviceConfigLimitSupportsConfiguredAndUnlimited(t *testing.T) {
	devices := make([]config.DeviceConfig, 2)

	err := validateFreeDeviceConfigLimit(devices, 2)
	if err == nil {
		t.Fatal("validateFreeDeviceConfigLimit() error = nil, want configured limit error")
	}
	if !strings.Contains(err.Error(), "2") {
		t.Fatalf("validateFreeDeviceConfigLimit() error = %q, want configured limit 2", err.Error())
	}

	if err := validateFreeDeviceConfigLimit(devices, 3); err != nil {
		t.Fatalf("validateFreeDeviceConfigLimit() below limit error = %v", err)
	}
	if err := validateFreeDeviceConfigLimit(devices, 0); err != nil {
		t.Fatalf("validateFreeDeviceConfigLimit() unlimited error = %v", err)
	}
}

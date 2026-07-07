package qmicore

import (
	"net"
	"testing"

	"github.com/boa-z/quectel-qmi-go/pkg/qmi"
)

func TestQmiSettingsIPv6(t *testing.T) {
	s := &qmi.RuntimeSettings{IPv6Address: net.ParseIP("2001:db8::1")}
	if got := qmiSettingsIPv6(s); got != "2001:db8::1" {
		t.Fatalf("qmiSettingsIPv6 = %q", got)
	}
	if got := qmiSettingsIPv6(nil); got != "" {
		t.Fatalf("nil settings should yield empty, got %q", got)
	}
}

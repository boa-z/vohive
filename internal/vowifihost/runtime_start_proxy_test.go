package vowifihost

import (
	"context"
	"testing"

	"github.com/boa-z/vowifi-go/runtimehost"
	"github.com/boa-z/vowifi-go/runtimehost/identity"
)

func TestManagerStartRuntimeInjectsSOCKS5TunnelManagerFactory(t *testing.T) {
	manager := NewManager()
	deviceID := "dev-proxy"
	claim := manager.BeginStart(deviceID)
	if !claim.Accepted {
		t.Fatalf("BeginStart() = %+v, want accepted", claim)
	}

	var captured runtimehost.StartRequest
	manager.SetRuntimeStartForTest(func(_ context.Context, req runtimehost.StartRequest) (*runtimehost.Instance, error) {
		captured = req
		return &runtimehost.Instance{}, nil
	})

	_, err := manager.StartRuntime(context.Background(), RuntimeStartRequest{
		DeviceID: deviceID,
		TraceID:  "trace-proxy",
		Epoch:    claim.Epoch,
		Prepared: PreparedStart{
			Profile: identity.Profile{IMSI: "234100000000001"},
			Prepared: identity.PreparedSession{
				Profile: identity.Profile{IMSI: "234100000000001"},
			},
			NetworkMode: "LTE",
			Proxy: &runtimehost.ProxyConfig{
				ID:      "giffgaff",
				Addr:    "127.0.0.1:7891",
				Enabled: true,
			},
		},
		Modem:     runtimeStartTestModem{},
		Dataplane: runtimehost.DataplanePolicy{Mode: "userspace"},
	})
	if err != nil {
		t.Fatalf("StartRuntime() error = %v", err)
	}
	if captured.TunnelManagerFactory == nil {
		t.Fatal("proxied StartRequest should inject a SOCKS5 tunnel manager factory")
	}
}

package device

import (
	"context"
	"testing"
	"time"

	qmimanager "github.com/boa-z/quectel-qmi-go/pkg/manager"
	"github.com/boa-z/quectel-qmi-go/pkg/qmi"
)

type reinitReadinessStub struct {
	readiness []qmimanager.UIMReadiness
	calls     int
}

func (s *reinitReadinessStub) GetUIMReadiness(ctx context.Context) (qmimanager.UIMReadiness, error) {
	if s.calls >= len(s.readiness) {
		return s.readiness[len(s.readiness)-1], s.readiness[len(s.readiness)-1].Err
	}
	out := s.readiness[s.calls]
	s.calls++
	return out, out.Err
}

type reinitPowerStub struct {
	off, on int
	offErr  error
	onErr   error
}

func (s *reinitPowerStub) UIMPowerOffSIM(ctx context.Context, slot uint8) error {
	s.off++
	return s.offErr
}

func (s *reinitPowerStub) UIMPowerOnSIM(ctx context.Context, slot uint8) error {
	s.on++
	return s.onErr
}

func TestSwitchEventSourceDeliversRefreshStage(t *testing.T) {
	src := newSwitchEventSource()
	src.PublishRefresh(uimRefreshStageEndWithSuccess)

	select {
	case ev := <-src.Events():
		if ev.Kind != switchEventRefresh || ev.RefreshStage != uimRefreshStageEndWithSuccess {
			t.Fatalf("got kind=%d stage=0x%02x", ev.Kind, ev.RefreshStage)
		}
	case <-time.After(time.Second):
		t.Fatal("expected an event, got none")
	}
}

func TestSwitchEventSourcePublishAfterCloseIsNoop(t *testing.T) {
	src := newSwitchEventSource()
	src.Close()
	src.PublishRefresh(uimRefreshStageStart) // must not panic
}

func TestSwitchEventSourceDropsWhenFull(t *testing.T) {
	src := newSwitchEventSource()
	// buffer is 16; publishing more must not block or panic
	for i := 0; i < 100; i++ {
		src.PublishSlotStatus()
	}
}

func TestAwaitReinitConvergedReturnsOnEndWithSuccess(t *testing.T) {
	src := newSwitchEventSource()
	rdy := &reinitReadinessStub{readiness: []qmimanager.UIMReadiness{{
		TransportReady: true, ControlReady: true, UIMReady: true,
		SIMStatus: qmi.SIMReady, Reason: qmimanager.UIMReadinessReady,
		ICCID: "89441000400308626482",
	}}}
	go func() {
		time.Sleep(20 * time.Millisecond)
		src.PublishRefresh(uimRefreshStageEndWithSuccess)
	}()

	res := awaitReinitConverged(context.Background(), src, rdy, "89441000400308626482", 2*time.Second)
	if res != reinitConverged {
		t.Fatalf("got %v want reinitConverged", res)
	}
}

func TestAwaitReinitConvergedTimesOutWithoutIndication(t *testing.T) {
	src := newSwitchEventSource()
	rdy := &reinitReadinessStub{readiness: []qmimanager.UIMReadiness{{
		TransportReady: true, ControlReady: true, UIMReady: false,
		Reason: qmimanager.UIMReadinessCardResetting,
	}}}
	res := awaitReinitConverged(context.Background(), src, rdy, "89441000400308626482", 150*time.Millisecond)
	if res != reinitTimeout {
		t.Fatalf("got %v want reinitTimeout", res)
	}
}

func TestTriggerPowerCycleFallbackCallsOffThenOn(t *testing.T) {
	p := &reinitPowerStub{}
	err := triggerPowerCycleFallback(context.Background(), p, 1)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if p.off != 1 || p.on != 1 {
		t.Fatalf("off=%d on=%d want 1/1", p.off, p.on)
	}
}

func TestTriggerPowerCycleFallbackStillCallsOnWhenOffFails(t *testing.T) {
	p := &reinitPowerStub{offErr: context.DeadlineExceeded}
	err := triggerPowerCycleFallback(context.Background(), p, 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if p.off != 1 || p.on != 1 {
		t.Fatalf("off=%d on=%d want 1/1", p.off, p.on)
	}
}

func TestTriggerPowerCycleFallbackNoControllerIsNoop(t *testing.T) {
	if err := triggerPowerCycleFallback(context.Background(), nil, 1); err != nil {
		t.Fatalf("want nil err for nil controller, got %v", err)
	}
}

func TestReinitPathHappyThenPowerCycleThenSafetyNet(t *testing.T) {
	src := newSwitchEventSource()
	rdy := &reinitReadinessStub{readiness: []qmimanager.UIMReadiness{{
		TransportReady: true, ControlReady: true, UIMReady: true,
		SIMStatus: qmi.SIMReady, Reason: qmimanager.UIMReadinessReady,
		ICCID: "89441000400308626482",
	}}}
	go func() {
		time.Sleep(10 * time.Millisecond)
		src.PublishRefresh(uimRefreshStageEndWithSuccess)
	}()

	if got := awaitReinitConverged(context.Background(), src, rdy, "89441000400308626482", time.Second); got != reinitConverged {
		t.Fatalf("happy path: got %v", got)
	}

	p := &reinitPowerStub{}
	if err := triggerPowerCycleFallback(context.Background(), p, 1); err != nil || p.off != 1 || p.on != 1 {
		t.Fatalf("power-cycle fallback: err=%v off=%d on=%d", err, p.off, p.on)
	}
}

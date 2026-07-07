package cscall

import (
	"context"
	"testing"

	"github.com/boa-z/quectel-qmi-go/pkg/qmi"
)

type fakeQMIVoiceSource struct {
	dialed       string
	answered     uint8
	ended        uint8
	info         *qmi.VoiceAllCallInfo
	statusHandle func(*qmi.VoiceAllCallInfo)
}

func (s *fakeQMIVoiceSource) VOICEDialCall(ctx context.Context, number string) (uint8, error) {
	s.dialed = number
	return 7, nil
}

func (s *fakeQMIVoiceSource) VOICEAnswerCall(ctx context.Context, callID uint8) (uint8, error) {
	s.answered = callID
	return callID, nil
}

func (s *fakeQMIVoiceSource) VOICEEndCall(ctx context.Context, callID uint8) (uint8, error) {
	s.ended = callID
	return callID, nil
}

func (s *fakeQMIVoiceSource) VOICEGetAllCallInfo(ctx context.Context) (*qmi.VoiceAllCallInfo, error) {
	return s.info, nil
}

func (s *fakeQMIVoiceSource) OnVoiceCallStatus(h func(*qmi.VoiceAllCallInfo)) error {
	s.statusHandle = h
	return nil
}

func TestQMIControllerDialAnswerHangup(t *testing.T) {
	src := &fakeQMIVoiceSource{}
	ctrl := NewQMIController(src)
	ref, err := ctrl.Dial(context.Background(), "10086")
	if err != nil {
		t.Fatalf("Dial() error=%v", err)
	}
	if ref.ID != "7" || src.dialed != "10086" {
		t.Fatalf("ref=%+v dialed=%q want id=7 number=10086", ref, src.dialed)
	}
	if err := ctrl.Answer(context.Background(), "7"); err != nil {
		t.Fatalf("Answer() error=%v", err)
	}
	if src.answered != 7 {
		t.Fatalf("answered=%d want 7", src.answered)
	}
	if err := ctrl.Hangup(context.Background(), "7", HangupOptions{SendModemSignal: true}); err != nil {
		t.Fatalf("Hangup() error=%v", err)
	}
	if src.ended != 7 {
		t.Fatalf("ended=%d want 7", src.ended)
	}
}

func TestQMIControllerMapsIncomingActiveAndHangup(t *testing.T) {
	src := &fakeQMIVoiceSource{}
	ctrl := NewQMIController(src)
	if err := ctrl.Start(context.Background()); err != nil {
		t.Fatalf("Start() error=%v", err)
	}
	src.statusHandle(&qmi.VoiceAllCallInfo{
		Calls:              []qmi.VoiceCallInfo{{ID: 3, State: qmi.VoiceCallState(0x02), Direction: qmi.VoiceCallDirection(1)}},
		RemotePartyNumbers: []qmi.VoiceRemotePartyNumber{{CallID: 3, Number: "+123"}},
	})
	got := <-ctrl.Events()
	if got.Type != EventIncoming || got.CallID != "3" || got.Number != "+123" {
		t.Fatalf("event=%+v want incoming call 3 +123", got)
	}

	src.statusHandle(&qmi.VoiceAllCallInfo{
		Calls: []qmi.VoiceCallInfo{{ID: 3, State: qmi.VoiceCallState(0x03), Direction: qmi.VoiceCallDirection(1)}},
	})
	got = <-ctrl.Events()
	if got.Type != EventConnected || got.CallID != "3" {
		t.Fatalf("event=%+v want connected call 3", got)
	}
	if ready := <-ctrl.PCMReady(); !ready {
		t.Fatal("PCMReady=false want true on active call")
	}

	src.statusHandle(&qmi.VoiceAllCallInfo{
		Calls: []qmi.VoiceCallInfo{{ID: 3, State: qmi.VoiceCallState(0x09), Direction: qmi.VoiceCallDirection(1)}},
	})
	got = <-ctrl.Events()
	if got.Type != EventHangup || got.CallID != "3" {
		t.Fatalf("event=%+v want hangup call 3", got)
	}
	if ready := <-ctrl.PCMReady(); ready {
		t.Fatal("PCMReady=true want false on hangup")
	}
}

func TestQMIControllerMapsLibQMIStates(t *testing.T) {
	info := &qmi.VoiceAllCallInfo{
		Calls: []qmi.VoiceCallInfo{
			{ID: 1, State: qmi.VoiceCallState(0x01), Direction: qmi.VoiceCallDirection(0)},
			{ID: 2, State: qmi.VoiceCallState(0x04), Direction: qmi.VoiceCallDirection(0)},
			{ID: 3, State: qmi.VoiceCallState(0x05), Direction: qmi.VoiceCallDirection(0)},
			{ID: 4, State: qmi.VoiceCallState(0x03), Direction: qmi.VoiceCallDirection(0)},
		},
	}
	calls := qmiCallInfos(info)
	if calls[0].State != CallStateDialing || calls[1].State != CallStateDialing {
		t.Fatalf("origination/cc-in-progress states=%v/%v want dialing", calls[0].State, calls[1].State)
	}
	if calls[2].State != CallStateRinging {
		t.Fatalf("alerting state=%v want ringing", calls[2].State)
	}
	if calls[3].State != CallStateConnected {
		t.Fatalf("conversation state=%v want connected", calls[3].State)
	}
}

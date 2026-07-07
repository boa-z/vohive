package qmicore

import (
	"fmt"
	"strings"
	"testing"

	qmimanager "github.com/boa-z/quectel-qmi-go/pkg/manager"
	"github.com/boa-z/quectel-qmi-go/pkg/qmi"
)

func summaryFieldsMap(fields []any) map[string]any {
	out := make(map[string]any, len(fields)/2)
	for i := 0; i+1 < len(fields); i += 2 {
		key, ok := fields[i].(string)
		if !ok {
			continue
		}
		out[key] = fields[i+1]
	}
	return out
}

func summaryContains(summary qmiEventSummary, needle string) bool {
	if strings.Contains(summary.Message, needle) {
		return true
	}
	for _, field := range summary.Fields {
		if strings.Contains(fmt.Sprint(field), needle) {
			return true
		}
	}
	return false
}

func TestSummarizeQMIEventNewSMS(t *testing.T) {
	summary := summarizeQMIEvent(qmimanager.Event{
		Type:        qmimanager.EventNewSMS,
		SMSIndex:    17,
		StorageType: 1,
	})

	if summary.Level != qmiEventLogInfo {
		t.Fatalf("level=%v want=%v", summary.Level, qmiEventLogInfo)
	}
	if summary.Message != "QMI 收到新短信指示" {
		t.Fatalf("message=%q", summary.Message)
	}

	fields := summaryFieldsMap(summary.Fields)
	if got := fields["index"]; got != uint32(17) {
		t.Fatalf("index=%v want=17", got)
	}
	if got := fields["storage"]; got != uint8(1) {
		t.Fatalf("storage=%v want=1", got)
	}
}

func TestSummarizeQMIEventNewSMSRawDoesNotLeakPDU(t *testing.T) {
	summary := summarizeQMIEvent(qmimanager.Event{
		Type: qmimanager.EventNewSMSRaw,
		Pdu:  []byte{0xde, 0xad, 0xbe, 0xef},
	})

	if summary.Level != qmiEventLogDebug {
		t.Fatalf("level=%v want=%v", summary.Level, qmiEventLogDebug)
	}

	fields := summaryFieldsMap(summary.Fields)
	if got := fields["pdu_len"]; got != 4 {
		t.Fatalf("pdu_len=%v want=4", got)
	}
	if summaryContains(summary, "deadbeef") {
		t.Fatal("summary leaked raw PDU content")
	}
}

func TestManagerDispatchesRawSMSIndication(t *testing.T) {
	m := &Manager{}
	gotCh := make(chan RawSMSIndication, 1)
	m.OnNewSMSRaw(func(info RawSMSIndication) {
		gotCh <- info
	})

	m.handleQMIEvent(qmimanager.Event{
		Type:             qmimanager.EventNewSMSRaw,
		Pdu:              []byte{0x04, 0x0b, 0x91},
		SMSAckRequired:   true,
		SMSTransactionID: 0x11223344,
		SMSFormat:        0x06,
	})

	select {
	case got := <-gotCh:
		if string(got.PDU) != string([]byte{0x04, 0x0b, 0x91}) {
			t.Fatalf("PDU=%x", got.PDU)
		}
		if !got.AckRequired {
			t.Fatal("AckRequired=false, want true")
		}
		if got.TransactionID != 0x11223344 {
			t.Fatalf("TransactionID=0x%x, want 0x11223344", got.TransactionID)
		}
		if got.Format != 0x06 {
			t.Fatalf("Format=0x%x, want 0x06", got.Format)
		}
	default:
		t.Fatal("raw SMS callback was not called")
	}
}

func TestSummarizeQMIEventVoiceUSSDDoesNotLeakText(t *testing.T) {
	summary := summarizeQMIEvent(qmimanager.Event{
		Type: qmimanager.EventVoiceUSSD,
		VoiceUSSD: &qmi.VoiceUSSDIndication{
			HasUserAction: true,
			UserAction:    3,
			USSData: &qmi.VoiceUSSDPayload{
				Text: "secret-ussd-text",
			},
		},
	})

	if summary.Level != qmiEventLogDebug {
		t.Fatalf("level=%v want=%v", summary.Level, qmiEventLogDebug)
	}

	fields := summaryFieldsMap(summary.Fields)
	if got := fields["user_action"]; got != qmi.VoiceUserAction(3) {
		t.Fatalf("user_action=%v want=3", got)
	}
	if got := fields["ussd_len"]; got != len("secret-ussd-text") {
		t.Fatalf("ussd_len=%v want=%d", got, len("secret-ussd-text"))
	}
	if summaryContains(summary, "secret-ussd-text") {
		t.Fatal("summary leaked USSD text")
	}
}

func TestSummarizeQMIEventVoiceSupplementaryRequestDoesNotLeakUSSDText(t *testing.T) {
	summary := summarizeQMIEvent(qmimanager.Event{
		Type: qmimanager.EventVoiceSupplementaryServiceRequest,
		VoiceSupplementaryRequest: &qmi.VoiceSupplementaryServiceRequestIndication{
			HasInfo:               true,
			Request:               0x07,
			ModifiedByCallControl: true,
			USSData: &qmi.VoiceUSSDPayload{
				Text: "secret-ussd-request",
			},
			EncodedDataUTF16: []uint16{0x004f, 0x004b},
		},
	})

	if summary.Level != qmiEventLogDebug {
		t.Fatalf("level=%v want=%v", summary.Level, qmiEventLogDebug)
	}

	fields := summaryFieldsMap(summary.Fields)
	if got := fields["request"]; got != uint8(0x07) {
		t.Fatalf("request=%v want=7", got)
	}
	if got := fields["modified_by_call_control"]; got != true {
		t.Fatalf("modified_by_call_control=%v want=true", got)
	}
	if got := fields["ussd_len"]; got != len("secret-ussd-request") {
		t.Fatalf("ussd_len=%v want=%d", got, len("secret-ussd-request"))
	}
	if got := fields["encoded_utf16_len"]; got != 2 {
		t.Fatalf("encoded_utf16_len=%v want=2", got)
	}
	if summaryContains(summary, "secret-ussd-request") {
		t.Fatal("summary leaked USSD text")
	}
}

func TestSummarizeQMIEventIMSRegistrationDoesNotLeakErrorMessage(t *testing.T) {
	summary := summarizeQMIEvent(qmimanager.Event{
		Type: qmimanager.EventIMSRegistrationStatus,
		IMSRegistration: &qmi.IMSARegistrationStatus{
			Status:          qmi.IMSARegistrationStateRegistered,
			HasStatus:       true,
			ErrorCode:       42,
			HasErrorCode:    true,
			Technology:      qmi.IMSARegistrationTechnologyWLAN,
			HasTechnology:   true,
			ErrorMessage:    "super-secret-registration-error",
			HasErrorMessage: true,
		},
	})

	fields := summaryFieldsMap(summary.Fields)
	if got := fields["error_message_len"]; got != len("super-secret-registration-error") {
		t.Fatalf("error_message_len=%v want=%d", got, len("super-secret-registration-error"))
	}
	if summaryContains(summary, "super-secret-registration-error") {
		t.Fatal("summary leaked IMS error message text")
	}
}

func TestSummarizeQMIEventWMSSMSCAddressDoesNotLeakDigits(t *testing.T) {
	summary := summarizeQMIEvent(qmimanager.Event{
		Type: qmimanager.EventWMSSMSCAddress,
		WMSSMSCAddress: &qmi.WMSSMSCAddress{
			Type:   "IPV",
			Digits: "8613800100500",
		},
	})

	fields := summaryFieldsMap(summary.Fields)
	if got := fields["type"]; got != "IPV" {
		t.Fatalf("type=%v want=IPV", got)
	}
	if got := fields["digits_len"]; got != len("8613800100500") {
		t.Fatalf("digits_len=%v want=%d", got, len("8613800100500"))
	}
	if summaryContains(summary, "8613800100500") {
		t.Fatal("summary leaked SMSC digits")
	}
}

func TestSummarizeQMIEventWMSTransportNetworkRegistrationStatus(t *testing.T) {
	summary := summarizeQMIEvent(qmimanager.Event{
		Type:                     qmimanager.EventWMSTransportNetworkRegistrationStatus,
		WMSTransportRegistration: qmi.WMSTransportNetworkRegistrationFullService,
	})

	fields := summaryFieldsMap(summary.Fields)
	if got := fields["registration_status"]; got != "full-service" {
		t.Fatalf("registration_status=%v want=full-service", got)
	}
}

func TestQMIRadioInterfaceNameMapsLTEValue(t *testing.T) {
	if got := qmiRadioInterfaceName(8); got != "lte" {
		t.Fatalf("qmiRadioInterfaceName(8)=%q want lte", got)
	}
}

func TestSummarizeQMIEventServingSystemChangedUsesStructuredFields(t *testing.T) {
	summary := summarizeQMIEvent(qmimanager.Event{
		Type: qmimanager.EventServingSystemChanged,
		ServingSystem: &qmi.ServingSystem{
			RegistrationState: qmi.RegStateRoaming,
			PSAttached:        true,
			RadioInterface:    6,
			MCC:               460,
			MNC:               1,
		},
		RawQMIType: qmi.EventServingSystemChanged,
		ServiceID:  qmi.ServiceNAS,
		MessageID:  qmi.NASServingSystemInd,
	})

	fields := summaryFieldsMap(summary.Fields)
	if got := fields["registration_state"]; got != "roaming" {
		t.Fatalf("registration_state=%v want=roaming", got)
	}
	if got := fields["ps_attached"]; got != true {
		t.Fatalf("ps_attached=%v want=true", got)
	}
	if got := fields["radio_interface"]; got != "nr5g" {
		t.Fatalf("radio_interface=%v want=nr5g", got)
	}
	if got := fields["mcc"]; got != uint16(460) {
		t.Fatalf("mcc=%v want=460", got)
	}
	if got := fields["mnc"]; got != uint16(1) {
		t.Fatalf("mnc=%v want=1", got)
	}
}

func TestSummarizeQMIEventPacketServiceStatusChangedUsesStatus(t *testing.T) {
	summary := summarizeQMIEvent(qmimanager.Event{
		Type:                qmimanager.EventPacketServiceStatusChanged,
		PacketServiceStatus: qmi.StatusConnected,
	})

	fields := summaryFieldsMap(summary.Fields)
	if got := fields["status"]; got != "connected" {
		t.Fatalf("status=%v want=connected", got)
	}
}

func TestSummarizeQMIEventUIMSessionClosedIsSuppressed(t *testing.T) {
	summary := summarizeQMIEvent(qmimanager.Event{
		Type:       qmimanager.EventUIMSessionClosed,
		RawQMIType: qmi.EventUIMSessionClosed,
		ServiceID:  qmi.ServiceUIM,
		MessageID:  qmi.UIMSessionClosedInd,
		TLVMeta: []qmi.TLVMeta{
			{Type: 0x13, Length: 4},
			{Type: 0x01, Length: 1},
			{Type: 0x11, Length: 1},
		},
	})

	if summary.Message != "" || len(summary.Fields) != 0 {
		t.Fatalf("summary=%+v, want suppressed normal UIM session close", summary)
	}
}

func TestSummarizeQMIEventEmptyNASEventReportIsSuppressed(t *testing.T) {
	summary := summarizeQMIEvent(qmimanager.Event{
		Type:       qmimanager.EventNASEventReport,
		RawQMIType: qmi.EventNASEventReport,
		ServiceID:  qmi.ServiceNAS,
		MessageID:  qmi.NASEventReportInd,
		TLVMeta:    nil,
	})

	if summary.Message != "" || len(summary.Fields) != 0 {
		t.Fatalf("summary=%+v, want suppressed empty NAS EventReport", summary)
	}
}

func TestSummarizeQMIEventUnknownIncludesRawMetadata(t *testing.T) {
	summary := summarizeQMIEvent(qmimanager.Event{
		Type:       qmimanager.EventUnknownIndication,
		RawQMIType: qmi.EventUnknown,
		ServiceID:  qmi.ServiceWMS,
		MessageID:  0x0044,
		TLVMeta: []qmi.TLVMeta{
			{Type: 0x01, Length: 16},
			{Type: 0x10, Length: 1},
		},
	})

	if summary.Level != qmiEventLogDebug {
		t.Fatalf("level=%v want=%v", summary.Level, qmiEventLogDebug)
	}
	if summary.Message != "QMI 收到未知指示" {
		t.Fatalf("message=%q", summary.Message)
	}

	fields := summaryFieldsMap(summary.Fields)
	if got := fields["raw_type"]; got != int(qmi.EventUnknown) {
		t.Fatalf("raw_type=%v want=%d", got, qmi.EventUnknown)
	}
	if got := fields["service_id"]; got != uint8(qmi.ServiceWMS) {
		t.Fatalf("service_id=%v want=%d", got, qmi.ServiceWMS)
	}
	if got := fields["service_name"]; got != "WMS" {
		t.Fatalf("service_name=%v want=WMS", got)
	}
	if got := fields["message_id"]; got != uint16(0x0044) {
		t.Fatalf("message_id=%v want=%d", got, 0x0044)
	}
	if got := fields["tlv_count"]; got != 2 {
		t.Fatalf("tlv_count=%v want=2", got)
	}
	if got := fields["tlvs"]; got != "0x01:16,0x10:1" {
		t.Fatalf("tlvs=%v want=%q", got, "0x01:16,0x10:1")
	}
	if summaryContains(summary, "secret") {
		t.Fatal("summary should not include unknown indication payload content")
	}
}

func TestSummarizeQMIEventDialFailedLevel(t *testing.T) {
	wantErr := fmt.Errorf("boom")
	summary := summarizeQMIEvent(qmimanager.Event{
		Type:  qmimanager.EventDialFailed,
		Error: wantErr,
	})

	if summary.Level != qmiEventLogWarn {
		t.Fatalf("level=%v want=%v", summary.Level, qmiEventLogWarn)
	}

	fields := summaryFieldsMap(summary.Fields)
	if got := fields["err"]; got != wantErr {
		t.Fatalf("err=%v want=%v", got, wantErr)
	}
}

func TestOnNewSMSWithStorageDispatchesStorageAndIndex(t *testing.T) {
	m := &Manager{}

	var gotStorage uint8
	var gotIndex uint32
	m.OnNewSMSWithStorage(func(storage uint8, index uint32) {
		gotStorage = storage
		gotIndex = index
	})

	m.handleQMIEvent(qmimanager.Event{
		Type:        qmimanager.EventNewSMS,
		StorageType: 1,
		SMSIndex:    42,
	})

	if gotStorage != 1 || gotIndex != 42 {
		t.Fatalf("got storage/index=(%d,%d) want=(1,42)", gotStorage, gotIndex)
	}
}

func TestOnNewSMSCompatibilityIgnoresStorage(t *testing.T) {
	m := &Manager{}

	var gotIndex uint32
	m.OnNewSMS(func(index uint32) {
		gotIndex = index
	})

	m.handleQMIEvent(qmimanager.Event{
		Type:        qmimanager.EventNewSMS,
		StorageType: 0,
		SMSIndex:    7,
	})

	if gotIndex != 7 {
		t.Fatalf("got index=%d want=7", gotIndex)
	}
}

func TestManagerDispatchesHealthEventsForQMIEvents(t *testing.T) {
	m := &Manager{}
	got := make([]HealthEvent, 0, 3)
	m.OnHealthEvent(func(event HealthEvent) {
		got = append(got, event)
	})

	m.handleQMIEvent(qmimanager.Event{Type: qmimanager.EventConnected})
	m.handleQMIEvent(qmimanager.Event{Type: qmimanager.EventReconnecting})
	m.handleQMIEvent(qmimanager.Event{Type: qmimanager.EventDisconnected})

	if len(got) != 3 {
		t.Fatalf("health events=%d want 3", len(got))
	}
	if got[0].State != HealthEventHealthy || got[0].Reason != "qmi_connected" {
		t.Fatalf("first health event=%+v want healthy qmi_connected", got[0])
	}
	if got[1].State != HealthEventSuspect || got[1].Reason != "qmi_reconnecting" {
		t.Fatalf("second health event=%+v want suspect qmi_reconnecting", got[1])
	}
	if got[2].State != HealthEventSuspect || got[2].Reason != "qmi_disconnected" {
		t.Fatalf("third health event=%+v want suspect qmi_disconnected", got[2])
	}
}

func TestManagerDispatchesModemResetHealthEventAndKeepsResetHandlers(t *testing.T) {
	m := &Manager{}
	gotHealth := make(chan HealthEvent, 1)
	gotReset := make(chan struct{}, 1)
	m.OnHealthEvent(func(event HealthEvent) {
		gotHealth <- event
	})
	m.OnModemReset(func() {
		gotReset <- struct{}{}
	})

	m.handleQMIEvent(qmimanager.Event{Type: qmimanager.EventModemReset})

	select {
	case event := <-gotHealth:
		if event.State != HealthEventRecovering || event.Reason != "qmi_modem_reset" {
			t.Fatalf("health event=%+v want recovering qmi_modem_reset", event)
		}
	default:
		t.Fatal("health event was not dispatched")
	}
	select {
	case <-gotReset:
	default:
		t.Fatal("modem reset handler was not called")
	}
}

func TestSummarizeQMIEventUIMRefreshUsesStructuredFields(t *testing.T) {
	summary := summarizeQMIEvent(qmimanager.Event{
		Type: qmimanager.EventUIMRefresh,
		UIMRefresh: &qmi.UIMRefreshIndication{
			Stage:       2,
			Mode:        1,
			SessionType: 0,
			Files: []qmi.UIMRefreshFile{
				{FileID: 0x6F07},
				{FileID: 0x6FAD},
			},
		},
		RawQMIType: qmi.EventUIMRefresh,
		ServiceID:  qmi.ServiceUIM,
		MessageID:  qmi.UIMRefreshInd,
	})

	if summary.Message != "QMI UIM refresh 指示" {
		t.Fatalf("message=%q", summary.Message)
	}
	fields := summaryFieldsMap(summary.Fields)
	if got := fields["stage"]; got != uint8(2) {
		t.Fatalf("stage=%v want=2", got)
	}
	if got := fields["file_count"]; got != 2 {
		t.Fatalf("file_count=%v want=2", got)
	}
	if got := fields["message_id"]; got != uint16(qmi.UIMRefreshInd) {
		t.Fatalf("message_id=%v want=%d", got, qmi.UIMRefreshInd)
	}
}

func TestSummarizeQMIEventUIMSlotStatusUsesStructuredFields(t *testing.T) {
	summary := summarizeQMIEvent(qmimanager.Event{
		Type: qmimanager.EventUIMSlotStatus,
		UIMSlotStatus: &qmi.UIMSlotStatus{
			Slots: []qmi.UIMSlotStatusSlot{
				{LogicalSlot: 1},
				{LogicalSlot: 2},
			},
		},
		RawQMIType: qmi.EventUIMSlotStatus,
		ServiceID:  qmi.ServiceUIM,
		MessageID:  qmi.UIMSlotStatusInd,
	})

	if summary.Message != "QMI UIM 卡槽状态指示" {
		t.Fatalf("message=%q", summary.Message)
	}
	fields := summaryFieldsMap(summary.Fields)
	if got := fields["slot_count"]; got != 2 {
		t.Fatalf("slot_count=%v want=2", got)
	}
	if got := fields["message_id"]; got != uint16(qmi.UIMSlotStatusInd) {
		t.Fatalf("message_id=%v want=%d", got, qmi.UIMSlotStatusInd)
	}
}

func TestHandleQMIEventDispatchesUIMCallbacks(t *testing.T) {
	m := &Manager{}

	var gotStage uint8
	var gotSlotCount int
	m.OnUIMRefresh(func(info *qmi.UIMRefreshIndication) {
		if info != nil {
			gotStage = info.Stage
		}
	})
	m.OnUIMSlotStatus(func(info *qmi.UIMSlotStatus) {
		if info != nil {
			gotSlotCount = len(info.Slots)
		}
	})

	m.handleQMIEvent(qmimanager.Event{
		Type:       qmimanager.EventUIMRefresh,
		UIMRefresh: &qmi.UIMRefreshIndication{Stage: 3},
	})
	m.handleQMIEvent(qmimanager.Event{
		Type: qmimanager.EventUIMSlotStatus,
		UIMSlotStatus: &qmi.UIMSlotStatus{
			Slots: []qmi.UIMSlotStatusSlot{{LogicalSlot: 1}},
		},
	})

	if gotStage != 3 {
		t.Fatalf("gotStage=%d want=3", gotStage)
	}
	if gotSlotCount != 1 {
		t.Fatalf("gotSlotCount=%d want=1", gotSlotCount)
	}
}

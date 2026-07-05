package device

import (
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/runtimehost/eventhost"
)

type VoWiFiUSSDEvent struct {
	DeviceID  string    `json:"device_id"`
	SessionID string    `json:"session_id"`
	Text      string    `json:"text,omitempty"`
	RawText   string    `json:"raw_text,omitempty"`
	Status    int       `json:"status,omitempty"`
	DCS       int       `json:"dcs,omitempty"`
	Done      bool      `json:"done"`
	Channel   string    `json:"channel"`
	Time      time.Time `json:"time"`
}

func (p *Pool) PublishVoWiFiUSSDUpdated(ev eventhost.USSDUpdated) VoWiFiUSSDEvent {
	out := VoWiFiUSSDEvent{
		DeviceID:  strings.TrimSpace(ev.DevID),
		SessionID: strings.TrimSpace(ev.SessionID),
		Text:      ev.Text,
		RawText:   ev.RawText,
		Status:    ev.Status,
		DCS:       ev.DCS,
		Done:      ev.Done,
		Channel:   "vowifi",
		Time:      ev.Time,
	}
	if out.Time.IsZero() {
		out.Time = time.Now()
	}
	if p == nil || out.DeviceID == "" {
		return out
	}

	p.vowifiUSSDMu.RLock()
	subs := p.vowifiUSSDSubs[out.DeviceID]
	listeners := make([]chan VoWiFiUSSDEvent, 0, len(subs))
	for _, ch := range subs {
		listeners = append(listeners, ch)
	}
	p.vowifiUSSDMu.RUnlock()

	for _, ch := range listeners {
		select {
		case ch <- out:
		default:
		}
	}
	return out
}

func (p *Pool) SubscribeVoWiFiUSSD(deviceID string) (<-chan VoWiFiUSSDEvent, func()) {
	if p == nil {
		return nil, func() {}
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return nil, func() {}
	}

	ch := make(chan VoWiFiUSSDEvent, 8)
	p.vowifiUSSDMu.Lock()
	if p.vowifiUSSDSubs == nil {
		p.vowifiUSSDSubs = make(map[string]map[uint64]chan VoWiFiUSSDEvent)
	}
	p.vowifiUSSDSeq++
	subID := p.vowifiUSSDSeq
	if p.vowifiUSSDSubs[deviceID] == nil {
		p.vowifiUSSDSubs[deviceID] = make(map[uint64]chan VoWiFiUSSDEvent)
	}
	p.vowifiUSSDSubs[deviceID][subID] = ch
	p.vowifiUSSDMu.Unlock()

	unsub := func() {
		p.vowifiUSSDMu.Lock()
		defer p.vowifiUSSDMu.Unlock()
		if subs, ok := p.vowifiUSSDSubs[deviceID]; ok {
			delete(subs, subID)
			if len(subs) == 0 {
				delete(p.vowifiUSSDSubs, deviceID)
			}
		}
	}
	return ch, unsub
}

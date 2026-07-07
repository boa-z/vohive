package stream

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type GatewayLookup interface {
	Gateway(context.Context) (string, error)
}

type TokenProvider interface {
	Token(context.Context) (string, error)
}

type Config struct {
	Lookup  GatewayLookup
	Tokens  TokenProvider
	Intents int
	Logger  *slog.Logger
}

type Pump struct {
	lookup  GatewayLookup
	tokens  TokenProvider
	intents int
	logger  *slog.Logger
	dialer  *websocket.Dialer
	wait    func(context.Context, time.Duration) error
}

type Envelope struct {
	ID   string
	Kind string
	From string
	To   Destination
	Text string
	At   time.Time
	Raw  json.RawMessage
}

type Destination struct {
	Kind string
	ID   string
}

type frame struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  *int64          `json:"s,omitempty"`
	T  string          `json:"t,omitempty"`
}

type hello struct {
	HeartbeatMS int64 `json:"heartbeat_interval"`
}

type directEvent struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Author    struct {
		User string `json:"user_openid"`
	} `json:"author"`
}

type groupEvent struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	GroupID   string    `json:"group_openid"`
	Author    struct {
		Member string `json:"member_openid"`
	} `json:"author"`
}

func New(cfg Config) *Pump {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	intents := cfg.Intents
	if intents <= 0 {
		intents = 1 << 25
	}
	return &Pump{
		lookup:  cfg.Lookup,
		tokens:  cfg.Tokens,
		intents: intents,
		logger:  logger,
		dialer:  websocket.DefaultDialer,
		wait:    pause,
	}
}

func (p *Pump) Run(ctx context.Context, consume func(context.Context, Envelope) error) error {
	if consume == nil {
		return errors.New("consume 不能为空")
	}

	delay := time.Second
	for {
		ready, err := p.runSession(ctx, consume)
		if ctx.Err() != nil {
			return nil
		}
		if err == nil {
			return nil
		}
		if err.Error() != "gateway requested reconnect" {
			p.logger.Warn("gateway stopped", "err", err)
		}

		current := delay
		if ready {
			delay = time.Second
		} else if delay < 30*time.Second {
			delay *= 2
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
		}

		if err := p.wait(ctx, current); err != nil {
			return nil
		}
	}
}

func (p *Pump) runSession(ctx context.Context, consume func(context.Context, Envelope) error) (bool, error) {
	token, err := p.tokens.Token(ctx)
	if err != nil {
		return false, err
	}
	wsURL, err := p.lookup.Gateway(ctx)
	if err != nil {
		return false, err
	}

	conn, _, err := p.dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return false, err
	}
	defer conn.Close()

	closeStop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-closeStop:
		}
	}()
	defer close(closeStop)

	var ready bool
	var seq atomic.Int64
	heartStop := make(chan struct{})
	defer close(heartStop)

	for {
		select {
		case <-ctx.Done():
			return ready, nil
		default:
		}

		_, raw, err := conn.ReadMessage()
		if err != nil {
			return ready, err
		}

		packet := frame{}
		if err := json.Unmarshal(raw, &packet); err != nil {
			continue
		}
		if packet.S != nil {
			seq.Store(*packet.S)
		}

		switch packet.Op {
		case 10:
			greeting := hello{}
			if err := json.Unmarshal(packet.D, &greeting); err != nil {
				return ready, err
			}
			if greeting.HeartbeatMS > 0 {
				go heartbeat(ctx, conn, greeting.HeartbeatMS, &seq, heartStop)
			}
			if err := identify(conn, token, p.intents); err != nil {
				return ready, err
			}
			ready = true
		case 11:
			continue
		case 7:
			return ready, errors.New("gateway requested reconnect")
		case 9:
			return ready, errors.New("gateway session invalid")
		case 0:
			envelope, ok := decode(packet.T, packet.D)
			if !ok {
				continue
			}
			if err := consume(ctx, envelope); err != nil {
				return ready, err
			}
		}
	}
}

func decode(kind string, raw json.RawMessage) (Envelope, bool) {
	switch strings.TrimSpace(kind) {
	case "C2C_MESSAGE_CREATE":
		event := directEvent{}
		if err := json.Unmarshal(raw, &event); err != nil {
			return Envelope{}, false
		}
		if strings.TrimSpace(event.Author.User) == "" {
			return Envelope{}, false
		}
		return Envelope{
			ID:   event.ID,
			Kind: "direct_message",
			From: event.Author.User,
			To: Destination{
				Kind: "direct",
				ID:   event.Author.User,
			},
			Text: event.Content,
			At:   event.Timestamp,
			Raw:  raw,
		}, true
	case "GROUP_AT_MESSAGE_CREATE":
		event := groupEvent{}
		if err := json.Unmarshal(raw, &event); err != nil {
			return Envelope{}, false
		}
		if strings.TrimSpace(event.GroupID) == "" {
			return Envelope{}, false
		}
		return Envelope{
			ID:   event.ID,
			Kind: "group_mention",
			From: event.Author.Member,
			To: Destination{
				Kind: "group",
				ID:   event.GroupID,
			},
			Text: event.Content,
			At:   event.Timestamp,
			Raw:  raw,
		}, true
	default:
		return Envelope{}, false
	}
}

func identify(conn *websocket.Conn, token string, intents int) error {
	return conn.WriteJSON(map[string]any{
		"op": 2,
		"d": map[string]any{
			"token":   "QQBot " + token,
			"intents": intents,
			"shard":   []int{0, 1},
		},
	})
}

func heartbeat(ctx context.Context, conn *websocket.Conn, everyMS int64, seq *atomic.Int64, stop <-chan struct{}) {
	ticker := time.NewTicker(time.Duration(everyMS) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			_ = conn.WriteJSON(map[string]any{
				"op": 1,
				"d":  seq.Load(),
			})
		}
	}
}

func pause(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

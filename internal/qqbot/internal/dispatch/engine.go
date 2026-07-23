package dispatch

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/zanescope/vohive/internal/qqbot/internal/stream"
)

type Config struct {
	Prefix      string
	DedupWindow time.Duration
	Store       SeenStore
}

type Engine struct {
	prefix string
	window time.Duration
	store  SeenStore
}

type Route struct {
	Incoming Incoming
	Command  *Command
}

type Incoming struct {
	ID   string
	Kind string
	From string
	To   Recipient
	Text string
	At   time.Time
	Raw  json.RawMessage
}

type Recipient struct {
	Kind string
	ID   string
}

type Command struct {
	Name     string
	Params   []string
	Original string
}

func New(cfg Config) *Engine {
	prefix := strings.TrimSpace(cfg.Prefix)
	if prefix == "" {
		prefix = "/"
	}
	window := cfg.DedupWindow
	if window <= 0 {
		window = 2 * time.Hour
	}
	store := cfg.Store
	if store == nil {
		store = NewMemorySeenStore()
	}
	return &Engine{
		prefix: prefix,
		window: window,
		store:  store,
	}
}

func (e *Engine) Route(event stream.Envelope) (Route, bool) {
	if e.duplicate(event.Kind, event.ID) {
		return Route{}, true
	}

	route := Route{
		Incoming: Incoming{
			ID:   event.ID,
			Kind: event.Kind,
			From: event.From,
			To: Recipient{
				Kind: event.To.Kind,
				ID:   event.To.ID,
			},
			Text: event.Text,
			At:   event.At,
			Raw:  event.Raw,
		},
	}

	text := normalizeText(event)
	if !strings.HasPrefix(text, e.prefix) {
		return route, false
	}

	fields := strings.Fields(text)
	if len(fields) == 0 {
		return route, false
	}

	name := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(fields[0], e.prefix)))
	if name == "" {
		return route, false
	}
	route.Command = &Command{
		Name:     name,
		Params:   append([]string(nil), fields[1:]...),
		Original: text,
	}
	return route, false
}

func (e *Engine) duplicate(kind string, id string) bool {
	return e.store.Seen(kind, id, time.Now(), e.window)
}

func normalizeText(event stream.Envelope) string {
	text := strings.TrimSpace(event.Text)
	if event.Kind != "group_mention" {
		return text
	}
	if strings.HasPrefix(text, "<@") {
		if index := strings.Index(text, ">"); index >= 0 {
			text = text[index+1:]
		}
	}
	if strings.HasPrefix(text, "@") {
		if index := strings.IndexAny(text, " \t\r\n"); index >= 0 {
			text = text[index+1:]
		}
	}
	return strings.TrimSpace(text)
}

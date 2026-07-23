package dispatch

import (
	"testing"
	"time"

	"github.com/zanescope/vohive/internal/qqbot/internal/stream"
)

func TestEngineParsesDirectCommand(t *testing.T) {
	t.Parallel()

	engine := New(Config{Prefix: "/", DedupWindow: time.Hour})
	route, drop := engine.Route(stream.Envelope{
		ID:   "msg-1",
		Kind: "direct_message",
		To: stream.Destination{
			Kind: "direct",
			ID:   "user-1",
		},
		Text: "/status modem-1",
	})
	if drop {
		t.Fatal("Route() drop = true, want false")
	}
	if route.Command == nil {
		t.Fatal("Route() command = nil")
	}
	if route.Command.Name != "status" {
		t.Fatalf("command name = %q, want %q", route.Command.Name, "status")
	}
	if len(route.Command.Params) != 1 || route.Command.Params[0] != "modem-1" {
		t.Fatalf("command params = %#v", route.Command.Params)
	}
}

func TestEngineParsesGroupMentionCommand(t *testing.T) {
	t.Parallel()

	engine := New(Config{Prefix: "/", DedupWindow: time.Hour})
	route, drop := engine.Route(stream.Envelope{
		ID:   "msg-2",
		Kind: "group_mention",
		To: stream.Destination{
			Kind: "group",
			ID:   "group-1",
		},
		Text: "<@bot> /send 123 hello",
	})
	if drop {
		t.Fatal("Route() drop = true, want false")
	}
	if route.Command == nil || route.Command.Name != "send" {
		t.Fatalf("command = %#v", route.Command)
	}
}

func TestEngineDeduplicates(t *testing.T) {
	t.Parallel()

	engine := New(Config{Prefix: "/", DedupWindow: 50 * time.Millisecond})
	_, drop := engine.Route(stream.Envelope{ID: "msg-3", Kind: "direct_message"})
	if drop {
		t.Fatal("first drop = true, want false")
	}
	_, drop = engine.Route(stream.Envelope{ID: "msg-3", Kind: "direct_message"})
	if !drop {
		t.Fatal("second drop = false, want true")
	}
	time.Sleep(70 * time.Millisecond)
	_, drop = engine.Route(stream.Envelope{ID: "msg-3", Kind: "direct_message"})
	if drop {
		t.Fatal("drop after window = true, want false")
	}
}

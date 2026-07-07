package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeTokens struct {
	current     string
	invalidated int
}

func (f *fakeTokens) Token(context.Context) (string, error) {
	return f.current, nil
}

func (f *fakeTokens) Invalidate() {
	f.invalidated++
	if f.current == "token-1" {
		f.current = "token-2"
	}
}

func TestClientSendRefreshesUnauthorizedAndBuildsMarkdown(t *testing.T) {
	t.Parallel()

	tokens := &fakeTokens{current: "token-1"}
	var gotAuth string
	var gotPath string
	var gotBody map[string]any
	hits := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		hits++
		if hits == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    40101,
				"message": "expired",
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":        "reply-1",
			"timestamp": "2026-03-29T14:00:00Z",
		})
	}))
	defer server.Close()

	client := New(Config{
		BaseURL:     server.URL,
		Client:      server.Client(),
		Tokens:      tokens,
		RetryCount:  0,
		RetryDelay:  time.Millisecond,
		DefaultKind: "text",
	})

	result, err := client.Send(context.Background(), SendRequest{
		RecipientKind: "group",
		RecipientID:   "group-1",
		ContentKind:   "markdown",
		Content:       "**ok**",
		Reply: &ReplyRequest{
			MessageID: "msg-1",
			Sequence:  2,
		},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if tokens.invalidated != 1 {
		t.Fatalf("invalidated = %d, want 1", tokens.invalidated)
	}
	if gotAuth != "QQBot token-2" {
		t.Fatalf("authorization = %q, want %q", gotAuth, "QQBot token-2")
	}
	if gotPath != "/v2/groups/group-1/messages" {
		t.Fatalf("path = %q, want %q", gotPath, "/v2/groups/group-1/messages")
	}
	if got, ok := gotBody["msg_type"].(float64); !ok || int(got) != 2 {
		t.Fatalf("msg_type = %#v, want 2", gotBody["msg_type"])
	}
	if gotBody["msg_id"] != "msg-1" {
		t.Fatalf("msg_id = %#v, want %#v", gotBody["msg_id"], "msg-1")
	}
	if got, ok := gotBody["msg_seq"].(float64); !ok || int(got) != 2 {
		t.Fatalf("msg_seq = %#v, want 2", gotBody["msg_seq"])
	}
	if result.ID != "reply-1" {
		t.Fatalf("result.ID = %q, want %q", result.ID, "reply-1")
	}
}

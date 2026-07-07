package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestSourceCachesToken(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "token-1",
			"expires_in":   7200,
		})
	}))
	defer server.Close()

	source, err := New(Config{
		AppID:     "app-id",
		AppSecret: "secret",
		Endpoint:  server.URL,
		Client:    server.Client(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	first, err := source.Token(ctx)
	if err != nil {
		t.Fatalf("Token() first error = %v", err)
	}
	second, err := source.Token(ctx)
	if err != nil {
		t.Fatalf("Token() second error = %v", err)
	}

	if first != "token-1" || second != "token-1" {
		t.Fatalf("unexpected tokens: %q %q", first, second)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("hits = %d, want 1", got)
	}
}

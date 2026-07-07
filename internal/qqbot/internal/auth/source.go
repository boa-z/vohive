package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	AppID     string
	AppSecret string
	Endpoint  string
	Client    *http.Client
}

type Source struct {
	client   *http.Client
	endpoint string
	appID    string
	secret   string

	mu      sync.RWMutex
	token   string
	expires time.Time
}

type tokenPayload struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   any    `json:"expires_in"`
}

func New(cfg Config) (*Source, error) {
	if strings.TrimSpace(cfg.AppID) == "" {
		return nil, errors.New("app_id 不能为空")
	}
	if strings.TrimSpace(cfg.AppSecret) == "" {
		return nil, errors.New("app_secret 不能为空")
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, errors.New("endpoint 不能为空")
	}
	if cfg.Client == nil {
		return nil, errors.New("client 不能为空")
	}

	return &Source{
		client:   cfg.Client,
		endpoint: strings.TrimSpace(cfg.Endpoint),
		appID:    cfg.AppID,
		secret:   cfg.AppSecret,
	}, nil
}

func (s *Source) Token(ctx context.Context) (string, error) {
	s.mu.RLock()
	if s.token != "" && time.Until(s.expires) > 5*time.Minute {
		token := s.token
		s.mu.RUnlock()
		return token, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && time.Until(s.expires) > 5*time.Minute {
		return s.token, nil
	}

	rawBody, _ := json.Marshal(map[string]string{
		"appId":        s.appID,
		"clientSecret": s.secret,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(rawBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("token request failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	payload := tokenPayload{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return "", errors.New("token 为空")
	}

	lifetime := seconds(payload.ExpiresIn)
	if lifetime <= 0 {
		lifetime = 7200
	}
	s.token = payload.AccessToken
	s.expires = time.Now().Add(time.Duration(lifetime) * time.Second)
	return s.token, nil
}

func (s *Source) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token = ""
	s.expires = time.Time{}
}

func seconds(raw any) int64 {
	switch value := raw.(type) {
	case float64:
		return int64(value)
	case int64:
		return value
	case int:
		return int64(value)
	case json.Number:
		n, err := value.Int64()
		if err == nil {
			return n
		}
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err == nil {
			return n
		}
	}
	return 0
}

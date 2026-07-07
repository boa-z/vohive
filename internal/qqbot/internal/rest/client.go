package rest

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
	"time"
)

type TokenProvider interface {
	Token(context.Context) (string, error)
	Invalidate()
}

type Config struct {
	BaseURL     string
	Client      *http.Client
	Tokens      TokenProvider
	RetryCount  int
	RetryDelay  time.Duration
	DefaultKind string
}

type Client struct {
	baseURL     string
	client      *http.Client
	tokens      TokenProvider
	retryCount  int
	retryDelay  time.Duration
	defaultKind string
}

type SendRequest struct {
	RecipientKind string
	RecipientID   string
	ContentKind   string
	Content       string
	Reply         *ReplyRequest
}

type ReplyRequest struct {
	MessageID string
	Sequence  int
	EventID   string
}

type SendResult struct {
	ID string
	At time.Time
}

type apiError struct {
	StatusCode int
	Code       int    `json:"code"`
	Message    string `json:"message"`
	TraceID    string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("qq api status=%d code=%d message=%s trace=%s", e.StatusCode, e.Code, e.Message, e.TraceID)
}

func (e *apiError) retryable() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500
}

type messageReply struct {
	ID        string `json:"id"`
	Timestamp any    `json:"timestamp"`
}

type gatewayReply struct {
	URL string `json:"url"`
}

func New(cfg Config) *Client {
	retryCount := cfg.RetryCount
	if retryCount < 0 {
		retryCount = 0
	}
	retryDelay := cfg.RetryDelay
	if retryDelay <= 0 {
		retryDelay = 200 * time.Millisecond
	}
	defaultKind := strings.TrimSpace(cfg.DefaultKind)
	if defaultKind == "" {
		defaultKind = "text"
	}

	return &Client{
		baseURL:     strings.TrimRight(cfg.BaseURL, "/"),
		client:      cfg.Client,
		tokens:      cfg.Tokens,
		retryCount:  retryCount,
		retryDelay:  retryDelay,
		defaultKind: defaultKind,
	}
}

func (c *Client) Gateway(ctx context.Context) (string, error) {
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/gateway", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "QQBot "+token)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		c.tokens.Invalidate()
		return c.Gateway(ctx)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("gateway request failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	reply := gatewayReply{}
	if err := json.Unmarshal(body, &reply); err != nil {
		return "", err
	}
	if strings.TrimSpace(reply.URL) == "" {
		return "", errors.New("gateway url 为空")
	}
	return reply.URL, nil
}

func (c *Client) Send(ctx context.Context, req SendRequest) (SendResult, error) {
	var lastErr error
	for attempt := 0; attempt <= c.retryCount; attempt++ {
		out, err := c.sendOnce(ctx, req)
		if err == nil {
			return out, nil
		}
		lastErr = err

		var apiErr *apiError
		if !errors.As(err, &apiErr) || !apiErr.retryable() || attempt == c.retryCount {
			return SendResult{}, err
		}

		timer := time.NewTimer(c.retryDelay * time.Duration(1<<attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return SendResult{}, ctx.Err()
		case <-timer.C:
		}
	}
	return SendResult{}, lastErr
}

func (c *Client) sendOnce(ctx context.Context, req SendRequest) (SendResult, error) {
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return SendResult{}, err
	}

	url, body, err := c.encode(req)
	if err != nil {
		return SendResult{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return SendResult{}, err
	}
	httpReq.Header.Set("Authorization", "QQBot "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return SendResult{}, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return SendResult{}, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		c.tokens.Invalidate()
		return c.sendOnce(ctx, req)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &apiError{
			StatusCode: resp.StatusCode,
			TraceID:    resp.Header.Get("x-tps-trace-id"),
		}
		_ = json.Unmarshal(raw, apiErr)
		return SendResult{}, apiErr
	}

	reply := messageReply{}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return SendResult{}, err
	}
	return SendResult{
		ID: reply.ID,
		At: parseMoment(reply.Timestamp),
	}, nil
}

func (c *Client) encode(req SendRequest) (string, []byte, error) {
	if strings.TrimSpace(req.RecipientID) == "" {
		return "", nil, errors.New("recipient_id 不能为空")
	}
	if strings.TrimSpace(req.Content) == "" {
		return "", nil, errors.New("content 不能为空")
	}

	contentKind := strings.TrimSpace(req.ContentKind)
	if contentKind == "" {
		contentKind = c.defaultKind
	}

	payload := map[string]any{}
	switch contentKind {
	case "text":
		payload["content"] = req.Content
		payload["msg_type"] = 0
	case "markdown":
		payload["markdown"] = map[string]any{"content": req.Content}
		payload["msg_type"] = 2
	default:
		return "", nil, errors.New("content_kind 只支持 text 或 markdown")
	}

	if req.Reply != nil {
		if strings.TrimSpace(req.Reply.MessageID) != "" {
			payload["msg_id"] = req.Reply.MessageID
		}
		if req.Reply.Sequence > 0 {
			payload["msg_seq"] = req.Reply.Sequence
		}
		if strings.TrimSpace(req.Reply.EventID) != "" {
			payload["event_id"] = req.Reply.EventID
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", nil, err
	}

	switch req.RecipientKind {
	case "c2c":
		return fmt.Sprintf("%s/v2/users/%s/messages", c.baseURL, req.RecipientID), body, nil
	case "group":
		return fmt.Sprintf("%s/v2/groups/%s/messages", c.baseURL, req.RecipientID), body, nil
	case "channel":
		return fmt.Sprintf("%s/channels/%s/messages", c.baseURL, req.RecipientID), body, nil
	default:
		return "", nil, errors.New("recipient_kind 只支持 c2c/group/channel")
	}
}

func parseMoment(raw any) time.Time {
	switch value := raw.(type) {
	case string:
		value = strings.TrimSpace(value)
		if value == "" {
			return time.Now()
		}
		if ts, err := time.Parse(time.RFC3339, value); err == nil {
			return ts
		}
		if n, err := strconv.ParseInt(value, 10, 64); err == nil {
			return guessTime(n)
		}
	case float64:
		return guessTime(int64(value))
	case int64:
		return guessTime(value)
	case int:
		return guessTime(int64(value))
	case json.Number:
		if n, err := value.Int64(); err == nil {
			return guessTime(n)
		}
	}
	return time.Now()
}

func guessTime(raw int64) time.Time {
	if raw <= 0 {
		return time.Now()
	}
	if raw > 1_000_000_000_000 {
		return time.UnixMilli(raw)
	}
	return time.Unix(raw, 0)
}

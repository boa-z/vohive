package qqbot

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	defaultTokenEndpoint = "https://bots.qq.com/app/getAppAccessToken"
	defaultBaseEndpoint  = "https://api.sgroup.qq.com"
	defaultTimeout       = 10 * time.Second
	defaultPrefix        = "/"
	defaultIntents       = 1 << 25
	defaultRetryCount    = 3
	defaultRetryDelay    = 200 * time.Millisecond
	defaultDedupWindow   = 2 * time.Hour
)

type Settings struct {
	AppID         string
	AppSecret     string
	TokenEndpoint string
	BaseEndpoint  string
	Timeout       time.Duration
	DefaultKind   MessageKind
	HTTPClient    *http.Client
	Logger        *slog.Logger
}

type Option func(*options)

type options struct {
	prefix         string
	intents        int
	retryCount     int
	retryDelay     time.Duration
	dedupWindow    time.Duration
	unknownCommand CommandHandler
}

func defaultOptions() options {
	return options{
		prefix:      defaultPrefix,
		intents:     defaultIntents,
		retryCount:  defaultRetryCount,
		retryDelay:  defaultRetryDelay,
		dedupWindow: defaultDedupWindow,
		unknownCommand: func(ctx context.Context, c Conversation, _ ParsedCommand) error {
			_, err := c.RespondText(ctx, "未知命令")
			return err
		},
	}
}

func (s Settings) normalized() Settings {
	if strings.TrimSpace(s.TokenEndpoint) == "" {
		s.TokenEndpoint = defaultTokenEndpoint
	}
	if strings.TrimSpace(s.BaseEndpoint) == "" {
		s.BaseEndpoint = defaultBaseEndpoint
	}
	if s.Timeout <= 0 {
		s.Timeout = defaultTimeout
	}
	if s.DefaultKind == "" {
		s.DefaultKind = PlainText
	}
	if s.Logger == nil {
		s.Logger = slog.Default()
	}
	return s
}

func (s Settings) validate() error {
	if strings.TrimSpace(s.AppID) == "" {
		return errors.New("app_id 不能为空")
	}
	if strings.TrimSpace(s.AppSecret) == "" {
		return errors.New("app_secret 不能为空")
	}
	switch s.DefaultKind {
	case "", PlainText, Markdown:
	default:
		return errors.New("default_kind 只支持 text 或 markdown")
	}
	return nil
}

func WithPrefix(prefix string) Option {
	return func(o *options) {
		prefix = strings.TrimSpace(prefix)
		if prefix != "" {
			o.prefix = prefix
		}
	}
}

func WithIntents(intents int) Option {
	return func(o *options) {
		if intents > 0 {
			o.intents = intents
		}
	}
}

func WithRetry(count int, delay time.Duration) Option {
	return func(o *options) {
		if count >= 0 {
			o.retryCount = count
		}
		if delay > 0 {
			o.retryDelay = delay
		}
	}
}

func WithDedupWindow(window time.Duration) Option {
	return func(o *options) {
		if window > 0 {
			o.dedupWindow = window
		}
	}
}

func WithUnknownCommand(handler CommandHandler) Option {
	return func(o *options) {
		if handler != nil {
			o.unknownCommand = handler
		}
	}
}

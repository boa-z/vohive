package qqbot

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zanescope/vohive/internal/qqbot/internal/auth"
	"github.com/zanescope/vohive/internal/qqbot/internal/dispatch"
	"github.com/zanescope/vohive/internal/qqbot/internal/rest"
	"github.com/zanescope/vohive/internal/qqbot/internal/stream"
)

type App struct {
	settings Settings
	options  options

	delivery  *rest.Client
	eventPump *stream.Pump
	engine    *dispatch.Engine

	mu       sync.RWMutex
	stop     context.CancelFunc
	running  bool
	commands map[string]CommandHandler
	onText   TextHandler
}

func New(settings Settings, opts ...Option) (*App, error) {
	settings = settings.normalized()
	if err := settings.validate(); err != nil {
		return nil, err
	}

	cfg := defaultOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	httpClient := settings.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: settings.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        128,
				MaxIdleConnsPerHost: 64,
				IdleConnTimeout:     90 * time.Second,
			},
		}
	}

	tokenSource, err := auth.New(auth.Config{
		AppID:     settings.AppID,
		AppSecret: settings.AppSecret,
		Endpoint:  settings.TokenEndpoint,
		Client:    httpClient,
	})
	if err != nil {
		return nil, err
	}

	delivery := rest.New(rest.Config{
		BaseURL:     settings.BaseEndpoint,
		Client:      httpClient,
		Tokens:      tokenSource,
		RetryCount:  cfg.retryCount,
		RetryDelay:  cfg.retryDelay,
		DefaultKind: string(settings.DefaultKind),
	})

	pump := stream.New(stream.Config{
		Lookup:  delivery,
		Tokens:  tokenSource,
		Intents: cfg.intents,
		Logger:  settings.Logger,
	})

	return &App{
		settings:  settings,
		options:   cfg,
		delivery:  delivery,
		eventPump: pump,
		engine: dispatch.New(dispatch.Config{
			Prefix:      cfg.prefix,
			DedupWindow: cfg.dedupWindow,
		}),
		commands: make(map[string]CommandHandler),
	}, nil
}

func (a *App) Send(ctx context.Context, delivery Delivery) (Receipt, error) {
	request, err := a.encodeDelivery(delivery)
	if err != nil {
		return Receipt{}, err
	}
	result, err := a.delivery.Send(ctx, request)
	if err != nil {
		return Receipt{}, err
	}
	return Receipt{ID: result.ID, At: result.At}, nil
}

func (a *App) Command(name string, handler CommandHandler) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || handler == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.commands[name] = handler
}

func (a *App) OnText(handler TextHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onText = handler
}

func (a *App) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context 不能为空")
	}

	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return errors.New("app 已启动")
	}
	runCtx, cancel := context.WithCancel(ctx)
	a.stop = cancel
	a.running = true
	a.mu.Unlock()

	err := a.eventPump.Run(runCtx, func(ctx context.Context, event stream.Envelope) error {
		routed, drop := a.engine.Route(event)
		if drop {
			return nil
		}

		conv := &conversation{
			source:  a,
			inbound: toIncoming(routed.Incoming),
			nextSeq: 1,
		}

		if routed.Command != nil {
			a.mu.RLock()
			handler, ok := a.commands[strings.ToLower(routed.Command.Name)]
			fallback := a.options.unknownCommand
			a.mu.RUnlock()
			if ok {
				return handler(ctx, conv, ParsedCommand{
					Name:     routed.Command.Name,
					Params:   append([]string(nil), routed.Command.Params...),
					Original: routed.Command.Original,
				})
			}
			if fallback != nil {
				return fallback(ctx, conv, ParsedCommand{
					Name:     routed.Command.Name,
					Params:   append([]string(nil), routed.Command.Params...),
					Original: routed.Command.Original,
				})
			}
			return nil
		}

		a.mu.RLock()
		handler := a.onText
		a.mu.RUnlock()
		if handler == nil {
			return nil
		}
		return handler(ctx, conv)
	})

	a.mu.Lock()
	a.stop = nil
	a.running = false
	a.mu.Unlock()

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	return err
}

func (a *App) Close() error {
	a.mu.Lock()
	stop := a.stop
	a.stop = nil
	a.running = false
	a.mu.Unlock()
	if stop != nil {
		stop()
	}
	return nil
}

func (a *App) encodeDelivery(delivery Delivery) (rest.SendRequest, error) {
	if strings.TrimSpace(delivery.To.ID) == "" {
		return rest.SendRequest{}, errors.New("recipient.id 不能为空")
	}
	if strings.TrimSpace(delivery.Body) == "" {
		return rest.SendRequest{}, errors.New("body 不能为空")
	}

	kind := delivery.Kind
	if kind == "" {
		kind = a.settings.DefaultKind
	}

	request := rest.SendRequest{
		RecipientID: delivery.To.ID,
		Content:     delivery.Body,
	}

	switch delivery.To.Kind {
	case DirectRecipient:
		request.RecipientKind = "c2c"
	case GroupRecipient:
		request.RecipientKind = "group"
	case ChannelRecipient:
		request.RecipientKind = "channel"
	default:
		return rest.SendRequest{}, errors.New("recipient.kind 只支持 direct/group/channel")
	}

	switch kind {
	case PlainText:
		request.ContentKind = "text"
	case Markdown:
		request.ContentKind = "markdown"
	default:
		return rest.SendRequest{}, errors.New("kind 只支持 text 或 markdown")
	}

	if delivery.Reply != nil {
		request.Reply = &rest.ReplyRequest{
			MessageID: strings.TrimSpace(delivery.Reply.MessageID),
			Sequence:  delivery.Reply.Sequence,
			EventID:   strings.TrimSpace(delivery.Reply.EventID),
		}
	}

	return request, nil
}

type conversation struct {
	source  *App
	inbound Incoming

	mu      sync.Mutex
	nextSeq int
}

func (c *conversation) Incoming() Incoming {
	return c.inbound
}

func (c *conversation) Respond(ctx context.Context, delivery Delivery) (Receipt, error) {
	c.mu.Lock()
	seq := c.nextSeq
	c.nextSeq++
	c.mu.Unlock()

	delivery.To = c.inbound.To
	if delivery.Reply == nil {
		delivery.Reply = &ReplyContext{}
	}
	if strings.TrimSpace(delivery.Reply.MessageID) == "" {
		delivery.Reply.MessageID = c.inbound.ID
	}
	if delivery.Reply.Sequence <= 0 {
		delivery.Reply.Sequence = seq
	}
	return c.source.Send(ctx, delivery)
}

func (c *conversation) RespondText(ctx context.Context, text string) (Receipt, error) {
	return c.Respond(ctx, Delivery{
		Kind: PlainText,
		Body: text,
	})
}

func toIncoming(frame dispatch.Incoming) Incoming {
	return Incoming{
		ID:   frame.ID,
		Kind: IncomingKind(frame.Kind),
		From: frame.From,
		To: Recipient{
			Kind: RecipientKind(frame.To.Kind),
			ID:   frame.To.ID,
		},
		Text: frame.Text,
		At:   frame.At,
		Raw:  frame.Raw,
	}
}

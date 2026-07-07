package qqbot

import (
	"context"
	"encoding/json"
	"time"
)

type MessageKind string

const (
	PlainText MessageKind = "text"
	Markdown  MessageKind = "markdown"
)

type RecipientKind string

const (
	DirectRecipient  RecipientKind = "direct"
	GroupRecipient   RecipientKind = "group"
	ChannelRecipient RecipientKind = "channel"
)

type Recipient struct {
	Kind RecipientKind
	ID   string
}

type ReplyContext struct {
	MessageID string
	Sequence  int
	EventID   string
}

type Delivery struct {
	To    Recipient
	Kind  MessageKind
	Body  string
	Reply *ReplyContext
}

type Receipt struct {
	ID string
	At time.Time
}

type IncomingKind string

const (
	DirectMessage IncomingKind = "direct_message"
	GroupMention  IncomingKind = "group_mention"
)

type Incoming struct {
	ID   string
	Kind IncomingKind
	From string
	To   Recipient
	Text string
	At   time.Time
	Raw  json.RawMessage
}

type ParsedCommand struct {
	Name     string
	Params   []string
	Original string
}

type Conversation interface {
	Incoming() Incoming
	Respond(ctx context.Context, delivery Delivery) (Receipt, error)
	RespondText(ctx context.Context, text string) (Receipt, error)
}

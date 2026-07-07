package qqbot

import "context"

type CommandHandler func(context.Context, Conversation, ParsedCommand) error

type TextHandler func(context.Context, Conversation) error

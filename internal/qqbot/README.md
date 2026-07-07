# qqbot

`qqbot` 是一个的 QQ Bot Go 库，目标是嵌入其他 Go 服务并支持：

- 主动发送消息
- 接收消息并分发 slash 命令
- 在同一会话内按被动回复语义连续回消息

当前版本提供这几个稳定入口：

- `App.Send`
- `App.Command`
- `App.OnText`
- `App.Run`
- `Conversation.Respond / RespondText`

## 最小发送示例

```go
app, err := qqbot.New(qqbot.Settings{
    AppID:        "你的 AppID",
    AppSecret:    "你的 AppSecret",
})
if err != nil {
    panic(err)
}

_, err = app.Send(context.Background(), qqbot.Delivery{
    To: qqbot.Recipient{
        Kind: qqbot.DirectRecipient,
        ID:   "user_openid",
    },
    Kind: qqbot.PlainText,
    Body: "hello",
})
```

## 命令机器人示例

```go
app, _ := qqbot.New(qqbot.Settings{
    AppID:        "你的 AppID",
    AppSecret:    "你的 AppSecret",
})

app.Command("status", func(ctx context.Context, c qqbot.Conversation, cmd qqbot.ParsedCommand) error {
    _, err := c.RespondText(ctx, "服务正常")
    return err
})

app.OnText(func(ctx context.Context, c qqbot.Conversation) error {
    _, err := c.RespondText(ctx, "请使用 /status")
    return err
})

_ = app.Run(context.Background())
```

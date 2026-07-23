package cscall

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/zanescope/quectel-qmi-go/pkg/qmi"
)

// qmiVoiceSource 定义 QMI 底层 Voice 服务相关的接口
type qmiVoiceSource interface {
	VOICEDialCall(ctx context.Context, number string) (uint8, error)        // 发起 QMI 拨号呼叫
	VOICEAnswerCall(ctx context.Context, callID uint8) (uint8, error)       // 接听 QMI 指定 ID 的呼叫
	VOICEEndCall(ctx context.Context, callID uint8) (uint8, error)          // 挂断/结束 QMI 指定 ID 的呼叫
	VOICEGetAllCallInfo(ctx context.Context) (*qmi.VoiceAllCallInfo, error) // 获取当前所有活跃呼叫的详细信息
	OnVoiceCallStatus(func(*qmi.VoiceAllCallInfo)) error                    // 注册呼叫状态变更的监听回调
}

// QMIController 实现基于 QMI (Qualcomm MSM Interface) 协议的电路域呼叫控制器
type QMIController struct {
	source   qmiVoiceSource // 底层 QMI 语音源接口
	events   chan Event     // 呼叫事件分发通道
	pcmReady chan bool      // PCM 音频就绪通道
}

// NewQMIController 创建并初始化一个 QMI 呼叫控制器
func NewQMIController(source qmiVoiceSource) *QMIController {
	return &QMIController{
		source:   source,
		events:   make(chan Event, 8),
		pcmReady: make(chan bool, 8),
	}
}

// Start 启动 QMI 呼叫控制器，注册 QMI Voice 呼叫状态监听，并根据连接挂断状态自动触发 PCM 状态切换
func (c *QMIController) Start(ctx context.Context) error {
	if c.source == nil {
		return errors.New("qmi voice source is nil")
	}
	return c.source.OnVoiceCallStatus(func(info *qmi.VoiceAllCallInfo) {
		for _, event := range qmiVoiceEvents(info) {
			c.emit(event)
			switch event.Type {
			case EventConnected:
				c.emitPCM(true)
			case EventHangup:
				c.emitPCM(false)
			}
		}
	})
}

// Stop 停止 QMI 控制器
func (c *QMIController) Stop() {}

// Dial 发起一个新的 QMI 外呼呼叫，返回呼叫的引用信息
func (c *QMIController) Dial(ctx context.Context, number string) (CallRef, error) {
	id, err := c.source.VOICEDialCall(ctx, number)
	if err != nil {
		return CallRef{}, err
	}
	return CallRef{ID: strconv.Itoa(int(id)), Number: number}, nil
}

// Answer 接听指定的 QMI 来电呼叫
func (c *QMIController) Answer(ctx context.Context, callID string) error {
	id, err := parseQMICallID(callID)
	if err != nil {
		return err
	}
	_, err = c.source.VOICEAnswerCall(ctx, id)
	return err
}

// Hangup 结束/挂断指定的 QMI 呼叫
func (c *QMIController) Hangup(ctx context.Context, callID string, opts HangupOptions) error {
	if !opts.SendModemSignal {
		return nil
	}
	id, err := parseQMICallID(callID)
	if err != nil {
		return err
	}
	_, err = c.source.VOICEEndCall(ctx, id)
	return err
}

// GetCalls 查询并返回当前所有活跃的 QMI 呼叫列表状态
func (c *QMIController) GetCalls(ctx context.Context) ([]CallInfo, error) {
	info, err := c.source.VOICEGetAllCallInfo(ctx)
	if err != nil {
		return nil, err
	}
	return qmiCallInfos(info), nil
}

// Events 返回 QMI 呼叫事件的只读通道
func (c *QMIController) Events() <-chan Event { return c.events }

// PCMReady 返回 PCM 就绪状态的只读通道
func (c *QMIController) PCMReady() <-chan bool { return c.pcmReady }

// emit 辅助非阻塞地推送呼叫事件
func (c *QMIController) emit(event Event) {
	select {
	case c.events <- event:
	default:
	}
}

// emitPCM 辅助非阻塞地推送 PCM 就绪状态
func (c *QMIController) emitPCM(ready bool) {
	select {
	case c.pcmReady <- ready:
	default:
	}
}

// parseQMICallID 解析字符串格式的呼叫 ID 并校验是否是合法的 uint8
func parseQMICallID(in string) (uint8, error) {
	n, err := strconv.Atoi(strings.TrimSpace(in))
	if err != nil || n < 0 || n > 255 {
		return 0, strconv.ErrSyntax
	}
	return uint8(n), nil
}

// qmiCallInfos 将底层 QMI 全部呼叫信息记录映射转换为外部通用的 CallInfo 结构切片
func qmiCallInfos(info *qmi.VoiceAllCallInfo) []CallInfo {
	if info == nil {
		return nil
	}
	out := make([]CallInfo, 0, len(info.Calls))
	for _, call := range info.Calls {
		out = append(out, CallInfo{
			ID:        strconv.Itoa(int(call.ID)),
			Number:    qmiRemoteNumber(info, call.ID),
			Direction: qmiDirection(call.Direction),
			State:     qmiCallState(call.State),
		})
	}
	return out
}

// qmiVoiceEvents 从底层的 QMI 状态记录中提取转换出所有发生状态改变的呼叫事件
func qmiVoiceEvents(info *qmi.VoiceAllCallInfo) []Event {
	if info == nil {
		return nil
	}
	events := make([]Event, 0, len(info.Calls))
	for _, call := range info.Calls {
		callID := strconv.Itoa(int(call.ID))
		number := qmiRemoteNumber(info, call.ID)
		eventType, ok := qmiCallEventType(call.State, call.Direction)
		if ok {
			events = append(events, Event{Type: eventType, CallID: callID, Number: number})
		}
	}
	return events
}

// qmiRemoteNumber 根据呼叫 ID 从遥远端号码记录中提取有效的对端号码；若没有则返回 "Unknown"
func qmiRemoteNumber(info *qmi.VoiceAllCallInfo, callID uint8) string {
	if info != nil {
		for _, number := range info.RemotePartyNumbers {
			if number.CallID == callID && strings.TrimSpace(number.Number) != "" {
				return strings.TrimSpace(number.Number)
			}
		}
	}
	return "Unknown"
}

// qmiDirection 映射 QMI 呼叫方向为 "in" (来电) 或 "out" (去电)
func qmiDirection(dir qmi.VoiceCallDirection) string {
	if dir == 1 {
		return "in"
	}
	return "out"
}

// qmiCallState 将 QMI 底层呼叫状态代码映射为内部定义的电路域呼叫状态 CallState
func qmiCallState(state qmi.VoiceCallState) CallState {
	switch uint8(state) {
	case 0x01, 0x04:
		return CallStateDialing
	case 0x02, 0x05, 0x07, 0x0A:
		return CallStateRinging
	case 0x03, 0x06:
		return CallStateConnected
	default:
		return CallStateIdle
	}
}

// qmiCallEventType 将 QMI 的呼叫状态与方向映射转换为系统标准的呼叫事件类型
func qmiCallEventType(state qmi.VoiceCallState, direction qmi.VoiceCallDirection) (EventType, bool) {
	switch uint8(state) {
	case 0x01, 0x04:
		return EventDialing, true
	case 0x02, 0x07, 0x0A:
		if direction == 1 {
			return EventIncoming, true
		}
		return EventRinging, true
	case 0x05:
		return EventRinging, true
	case 0x03:
		return EventConnected, true
	case 0x08, 0x09:
		return EventHangup, true
	default:
		return "", false
	}
}

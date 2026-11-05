package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type piEventStream struct {
	ctx               context.Context
	events            chan<- aiEvent
	pending           []byte
	textSeen, anyText bool
	failure           error
}

func (s *piEventStream) emit(e aiEvent) error {
	select {
	case s.events <- e:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}
func (s *piEventStream) Write(data []byte) (int, error) {
	s.pending = append(s.pending, data...)
	for {
		i := bytes.IndexByte(s.pending, '\n')
		if i < 0 {
			break
		}
		line := s.pending[:i]
		s.pending = s.pending[i+1:]
		if err := s.line(line); err != nil {
			return 0, err
		}
	}
	if len(s.pending) > 16<<20 {
		return 0, fmt.Errorf("Pi 事件超过大小限制")
	}
	return len(data), nil
}
func (s *piEventStream) flush() error {
	if len(bytes.TrimSpace(s.pending)) > 0 {
		if err := s.line(s.pending); err != nil {
			return err
		}
	}
	s.pending = nil
	return s.failure
}
func (s *piEventStream) text(delta string) error {
	if delta == "" {
		return nil
	}
	if !s.textSeen && s.anyText {
		delta = "\n\n" + delta
	}
	s.textSeen = true
	s.anyText = true
	return s.emit(aiEvent{text: delta})
}
func (s *piEventStream) line(line []byte) error {
	if len(bytes.TrimSpace(line)) == 0 {
		return nil
	}
	var e struct {
		Type, ToolName, ToolCallID string
		IsError                    bool
		Args                       struct{ Path, Command, Pattern string }
		AssistantMessageEvent      struct{ Type, Delta string }
		Message                    struct {
			Role, StopReason, ErrorMessage string
			Content                        []struct{ Type, Text string }
		}
	}
	if err := json.Unmarshal(line, &e); err != nil {
		return fmt.Errorf("Pi 返回了无效的事件数据")
	}
	switch e.Type {
	case "agent_start":
		return s.emit(aiEvent{progress: "正在连接模型…"})
	case "message_start":
		if e.Message.Role == "assistant" {
			s.textSeen = false
		}
	case "message_update":
		switch e.AssistantMessageEvent.Type {
		case "text_delta":
			return s.text(e.AssistantMessageEvent.Delta)
		case "thinking_start":
			return s.emit(aiEvent{progress: "正在思考…"})
		}
	case "message_end":
		if e.Message.Role != "assistant" {
			return nil
		}
		if e.Message.StopReason == "error" || e.Message.StopReason == "aborted" {
			s.failure = fmt.Errorf("Pi：%s", e.Message.ErrorMessage)
			if e.Message.ErrorMessage == "" {
				s.failure = fmt.Errorf("Pi 请求未完成：%s", e.Message.StopReason)
			}
		}
		if e.Message.StopReason != "error" && e.Message.StopReason != "aborted" {
			s.failure = nil
		}
		if !s.textSeen {
			for _, part := range e.Message.Content {
				if part.Type == "text" {
					if err := s.text(part.Text); err != nil {
						return err
					}
				}
			}
		}
	case "tool_execution_start", "tool_execution_update", "tool_execution_end":
		verb := map[string]string{"read": "读取", "write": "写入", "edit": "修改", "bash": "执行命令", "grep": "搜索", "find": "查找", "ls": "浏览"}[e.ToolName]
		if verb == "" {
			verb = e.ToolName
		}
		target := e.Args.Path
		if target == "" {
			target = e.Args.Pattern
		}
		// Command text and tool output may contain secrets; show the action only.
		label := verb
		if target != "" {
			label += " · " + target
		}
		label = strings.ReplaceAll(label, "\n", " ")
		if len([]rune(label)) > 180 {
			label = string([]rune(label)[:180]) + "…"
		}
		done := e.Type == "tool_execution_end"
		return s.emit(aiEvent{progress: label, toolID: e.ToolCallID, toolDone: done, toolFailed: e.IsError})
	}
	return nil
}

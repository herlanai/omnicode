package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type buddyBlock struct {
	Type, Text, ID, Name string
	ToolUseID            string `json:"tool_use_id"`
	IsError              bool   `json:"is_error"`
	Input                struct {
		Path    string `json:"file_path"`
		Pattern string
	}
}

func (s *aiEventStream) buddyTool(b buddyBlock) error {
	if b.Type == "tool_result" {
		return s.emit(aiEvent{progress: "工具执行结束", toolID: b.ToolUseID, toolDone: true, toolFailed: b.IsError})
	}
	label := map[string]string{"Read": "读取文件", "Write": "写入文件", "Edit": "修改文件", "Grep": "搜索内容", "Glob": "查找文件", "Bash": "执行命令"}[b.Name]
	if label == "" {
		label = b.Name
	}
	if b.Input.Path != "" {
		label += " · " + strings.ReplaceAll(b.Input.Path, "\n", " ")
	}
	return s.emit(aiEvent{progress: label, toolID: b.ID})
}
func (s *aiEventStream) buddyLine(line []byte) error {
	// Configured CLI wrappers can still emit ordinary text.
	if !bytes.HasPrefix(bytes.TrimSpace(line), []byte("{")) {
		return s.emit(aiEvent{text: string(line)})
	}
	var e struct {
		Type, Subtype, Result string
		Parent                string `json:"parent_tool_use_id"`
		IsError               bool   `json:"is_error"`
		Errors                []string
		Event                 struct {
			Type  string
			Block buddyBlock `json:"content_block"`
			Delta struct{ Type, Text string }
		}
		Message struct{ Content json.RawMessage }
	}
	if err := json.Unmarshal(line, &e); err != nil {
		return fmt.Errorf("CodeBuddy 返回了无效的流式事件：%w", err)
	}
	switch e.Type {
	case "system":
		if e.Subtype == "init" {
			return s.emit(aiEvent{progress: "正在连接 CodeBuddy…"})
		}
	case "stream_event":
		if e.Parent != "" {
			return nil
		}
		switch e.Event.Type {
		case "message_start":
			s.textSeen = false
			s.buddyPartial = false
		case "content_block_start":
			b := e.Event.Block
			switch b.Type {
			case "thinking":
				return s.emit(aiEvent{progress: "正在思考…"})
			case "text":
				s.buddyPartial = b.Text != "" || s.buddyPartial
				return s.text(b.Text)
			case "tool_use":
				return s.buddyTool(b)
			}
		case "content_block_delta":
			if e.Event.Delta.Type == "text_delta" {
				s.buddyPartial = e.Event.Delta.Text != "" || s.buddyPartial
				return s.text(e.Event.Delta.Text)
			}
		}
	case "assistant", "user":
		if e.Parent != "" || !bytes.HasPrefix(bytes.TrimSpace(e.Message.Content), []byte("[")) {
			return nil
		}
		var blocks []buddyBlock
		if err := json.Unmarshal(e.Message.Content, &blocks); err != nil {
			return err
		}
		// Snapshots repeat streamed text but also supply complete tool names and paths.
		fallback := !s.buddyPartial
		for _, b := range blocks {
			var err error
			switch b.Type {
			case "text":
				if e.Type == "assistant" && fallback {
					err = s.text(b.Text)
				}
			case "tool_use", "tool_result":
				err = s.buddyTool(b)
			}
			if err != nil {
				return err
			}
		}
	case "result":
		if e.IsError || strings.HasPrefix(e.Subtype, "error") {
			detail := e.Result
			if detail == "" {
				detail = strings.Join(e.Errors, "；")
			}
			if detail == "" {
				detail = e.Subtype
			}
			s.failure = fmt.Errorf("CodeBuddy：%s", detail)
		} else if !s.anyText {
			return s.text(e.Result)
		}
	}
	return nil
}

package main

import (
	"context"
	"strings"
	"testing"
)

func TestPiStreamChunksAndNoDuplicates(t *testing.T) {
	events := make(chan aiEvent, 20)
	s := &aiEventStream{ctx: context.Background(), events: events}
	input := `{"type":"message_start","message":{"role":"assistant"}}
{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"你好"}}
{"type":"tool_execution_start","toolCallId":"one","toolName":"edit","args":{"path":"main.go"}}
{"type":"tool_execution_end","toolCallId":"one","toolName":"edit","isError":false}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"你好"}]}}
`
	for _, b := range []byte(input) {
		if _, err := s.Write([]byte{b}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.flush(); err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	var tools []aiEvent
	for len(events) > 0 {
		e := <-events
		text.WriteString(e.text)
		if e.toolID != "" {
			tools = append(tools, e)
		}
	}
	if text.String() != "你好" {
		t.Fatal("duplicate or broken deltas", text.String())
	}
	if len(tools) != 2 || tools[0].progress != "修改 · main.go" || !tools[1].toolDone {
		t.Fatal("tool progress missing")
	}
}
func TestPiStreamFallbackAndErrors(t *testing.T) {
	events := make(chan aiEvent, 10)
	s := &aiEventStream{ctx: context.Background(), events: events}
	s.Write([]byte(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"fallback"}]}}`))
	if err := s.flush(); err != nil {
		t.Fatal(err)
	}
	if (<-events).text != "fallback" {
		t.Fatal("final line lost")
	}
	s.Write([]byte("{\"type\":\"message_end\",\"message\":{\"role\":\"assistant\",\"stopReason\":\"error\",\"errorMessage\":\"authentication failed\"}}\n"))
	if err := s.flush(); err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatal("JSON-mode API failure hidden")
	}
	if _, err := s.Write([]byte("not-json\n")); err == nil {
		t.Fatal("invalid stream silently accepted")
	}
}
func TestPiStreamCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &aiEventStream{ctx: ctx, events: make(chan aiEvent)}
	if _, err := s.Write([]byte("{\"type\":\"message_update\",\"assistantMessageEvent\":{\"type\":\"text_delta\",\"delta\":\"x\"}}\n")); err == nil {
		t.Fatal("cancelled stream blocked")
	}
}

func TestPiProgressUpdatesConversation(t *testing.T) {
	a := newApp("")
	a.ai.running = true
	a.ai.messages = []aiMessage{{role: "assistant"}}
	a.ai.events = make(chan aiEvent, 4)
	a.ai.events <- aiEvent{progress: "写入 · main.go", toolID: "write-1"}
	a.ai.events <- aiEvent{text: "正在完成修改"}
	a.drainAI()
	if a.ai.messages[0].text != "正在完成修改" || len(a.ai.activities) != 1 || a.ai.activities[0].toolDone {
		t.Fatal("live conversation not updated")
	}
	a.ai.events <- aiEvent{progress: "写入", toolID: "write-1", toolDone: true}
	a.drainAI()
	if len(a.ai.activities) != 1 || !a.ai.activities[0].toolDone || a.ai.activities[0].progress != "写入 · main.go" {
		t.Fatal("tool completion lost its target")
	}
}

package main

import (
	"image/png"
	"os"
	"testing"

	"github.com/HyperparameterSI/golet/backend/ui"
)

func TestAIReplyWrappers(t *testing.T) {
	for _, tc := range []struct{ raw, body, thought string }{
		{"<analysis>分析内容</analysis>\n<final>正式回复</final>", "正式回复", "分析内容"},
		{"<think>尚在思考", "", "尚在思考"},
		{"正文\n<anal", "正文", ""},
		{"<analysis>内容</anal", "", "内容"},
		{"`<analysis>` 是标签", "`<analysis>` 是标签", ""},
		{"```xml\n<analysis>保留代码</analysis>\n```", "```xml\n<analysis>保留代码</analysis>\n```", ""},
		{"<answer>保留 <Component /> 和 a < b</answer>", "保留 <Component /> 和 a < b", ""},
	} {
		body, thought := splitAIReply(tc.raw)
		if body != tc.body || thought != tc.thought {
			t.Fatalf("%q => %q / %q", tc.raw, body, thought)
		}
	}
}
func TestAIReplyReadableLayout(t *testing.T) {
	a := newApp("")
	a.ai.visible = true
	a.ai.messages = []aiMessage{{"user", "检查项目并说明结果"}, {"assistant", "<analysis>先检查目录，再确认工具执行结果。</analysis>\n<final>## 检查结果\n\n当前任务未完成：工具没有获得执行权限。\n\n### 已完成\n\n1. 确认了项目路径和任务范围。\n2. 检查了返回信息，保留原文件。\n\n### 下一步\n\n请在引擎中确认所需权限，然后重试。\n\n```text\nPermissionDeniedError: tool execution was denied.\n```\n\n不会把未执行的操作报告为成功。</final>"}}
	tester := ui.NewTester(a.view, 1440, 900)
	if tester.HasText("<analysis>") {
		t.Fatal("raw wrapper visible")
	}
	if err := tester.Click("Reasoning"); err != nil {
		t.Fatal(err)
	}
	if !a.ai.thoughtOpen[1] {
		t.Fatal("disclosure did not open")
	}
	if os.Getenv("OMNICODE_AI_SCREENSHOT") != "" {
		os.MkdirAll("artifacts", 0755)
		f, err := os.Create("artifacts/ai-reply.png")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		png.Encode(f, tester.Image())
	}
}

package main

import (
	"strings"

	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/forma"
)

// Strip only model protocol wrappers, never arbitrary HTML or tags inside code.
// Incomplete wrappers are withheld while streaming to avoid flashing markup.
func splitAIReply(src string) (answer, thought string) {
	var body, reason strings.Builder
	depth := 0
	codeMark := byte(0)
	codeLen := 0
	write := func(s string) {
		if depth > 0 {
			reason.WriteString(s)
		} else {
			body.WriteString(s)
		}
	}
	for i := 0; i < len(src); {
		ch := src[i]
		if ch == '`' || (ch == '~' && (i == 0 || src[i-1] == '\n')) {
			n := 1
			for i+n < len(src) && src[i+n] == ch {
				n++
			}
			if codeLen == 0 && (ch == '`' || n >= 3) {
				codeMark = ch
				codeLen = n
			} else if codeMark == ch && n >= codeLen {
				codeLen = 0
			}
			write(src[i : i+n])
			i += n
			continue
		}
		if ch == '<' && codeLen == 0 {
			end := strings.IndexByte(src[i:], '>')
			if end >= 0 && end < 32 {
				tag := strings.ToLower(strings.TrimSpace(src[i+1 : i+end]))
				closing := strings.HasPrefix(tag, "/")
				name := strings.TrimPrefix(tag, "/")
				switch name {
				case "analysis", "think", "thinking", "reasoning":
					if closing {
						depth = max(0, depth-1)
					} else {
						depth++
					}
					i += end + 1
					continue
				case "final", "answer", "response":
					i += end + 1
					continue
				}
			} else if end < 0 {
				tail := strings.ToLower(src[i:])
				for _, tag := range []string{"analysis", "think", "thinking", "reasoning", "final", "answer", "response"} {
					if strings.HasPrefix("<"+tag+">", tail) || strings.HasPrefix("</"+tag+">", tail) {
						return strings.TrimSpace(body.String()), strings.TrimSpace(reason.String())
					}
				}
			}
		}
		write(src[i : i+1])
		i++
	}
	return strings.TrimSpace(body.String()), strings.TrimSpace(reason.String())
}

func (a *app) aiReply(c *ui.Context, index int, raw string) {
	body, thought := splitAIReply(raw)
	running := a.ai.running && index == len(a.ai.messages)-1
	if thought != "" {
		if a.ai.thoughtOpen == nil {
			a.ai.thoughtOpen = map[int]bool{}
		}
		open, seen := a.ai.thoughtOpen[index]
		if !seen {
			open = body == ""
		}
		forma.Reasoning(c.Key("reasoning"), thought, running, 0, &open)
		a.ai.thoughtOpen[index] = open
	}
	if body != "" {
		forma.AIMessage(c.Key("reply-body"), forma.MessageAssistant, body, forma.AIMessageOptions{Streaming: running})
	} else if running && thought == "" {
		muted(c, "正在处理…")
	}
}

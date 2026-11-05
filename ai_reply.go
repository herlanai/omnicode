package main

import (
	"strings"

	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/markdown"
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
	theme := markdown.DefaultTheme(c.Theme())
	theme.FontSize = max(float32(14), c.Theme().FontSize)
	theme.LineHeight = 1.65
	theme.BlockSpacing = 1
	theme.ItemSpacing = .5
	theme.ListIndent = 1.4
	theme.CodeSize = .9
	theme.CodeLineHeight = 1.5
	theme.CodeBlockPadding = .9
	options := &markdown.Options{Theme: theme, WrapCode: true}
	if thought != "" && body != "" {
		if a.ai.thoughtOpen == nil {
			a.ai.thoughtOpen = map[int]bool{}
		}
		open := a.ai.thoughtOpen[index]
		ui.Button(c.Key("thought-toggle"), "").FillWidth().Height(28).Padding(0).Border(0, ui.Transparent).Background(ui.Transparent).
			Label("思考过程").OnClick(func() { a.ai.thoughtOpen[index] = !open }).Children(func() {
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				name := "chevron-right"
				if open {
					name = "chevron-down"
				}
				icon(c, name, 12)
				muted(c, "思考过程").FontSize(12)
			})
		})
		if open {
			ui.Column(c.Key("thought-body")).FillWidth().Padding(0, 0, 8, 12).BorderWidth(0, 0, 0, 2).BorderColor(c.Theme().Border).Children(func() { markdown.View(c, thought, options).FillWidth().MinWidth(0) })
		}
	}
	// Some engines return only an analysis wrapper. Keep its content readable
	// instead of dropping the entire response or leaving an empty conversation.
	if body == "" {
		body = thought
	}
	if body != "" {
		markdown.View(c.Key("reply-body"), body, options).FillWidth().MinWidth(0)
	} else if running {
		muted(c, "正在处理…")
	}
}

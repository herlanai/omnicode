package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	golet "github.com/HyperparameterSI/golet/backend"
	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/editor"
)

const httpExample = `@baseUrl = http://localhost:8080

### 健康检查
GET {{baseUrl}}/health
Accept: application/json

### 创建示例
POST {{baseUrl}}/items
Content-Type: application/json

{"name": "example"}
`

type httpPanel struct {
	target        string
	targetVersion uint64
	targetCursor  int
	targetReady   bool
	split         float32
	cancel        context.CancelFunc
	done          chan httpReply
	reply         httpReply
	err           string
	headers       bool
	output        *editor.State
}

func isHTTPDocument(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".http" || ext == ".rest"
}

func (a *app) openHTTP() {
	if a.root == "" {
		a.git.visible = false
		a.quick = false
		for i, d := range a.docs {
			if d.path == "" && isHTTPDocument(d.name) {
				a.active = i
				return
			}
		}
		a.docs = append(a.docs, newDocument("requests.http", httpExample))
		a.active = len(a.docs) - 1
		a.focusEditor = true
		return
	}
	path := filepath.Join(a.root, "requests.http")
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		if a.git.mutating {
			a.notice = "请等待 Git 操作完成"
			return
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			a.report(err)
			return
		}
		_, err = f.WriteString(httpExample)
		closeErr := f.Close()
		if err != nil {
			a.report(err)
			return
		}
		if closeErr != nil {
			a.report(closeErr)
			return
		}
		a.refresh()
	}
	a.open(path)
}

func (p *httpPanel) send(d *document, start, end int) {
	if p.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	req, label, err := requestInRange(ctx, d.state.Buffer.String(), start, end)
	p.err = ""
	if err != nil {
		p.reply = httpReply{}
		p.output = nil
		cancel()
		p.err = err.Error()
		return
	}
	ch := make(chan httpReply, 1)
	p.cancel = cancel
	p.done = ch
	go func() { defer cancel(); ch <- executeHTTP(req, label) }()
}

func (p *httpPanel) showOutput() {
	text := p.reply.body
	if p.headers {
		text = p.reply.headers
	}
	p.output = &editor.State{Buffer: editor.NewBuffer(text), ReadOnly: true, SoftWrap: true, Label: "HTTP 响应"}
}

func (a *app) httpContent(c *ui.Context, d *document) {
	if d.http == nil {
		d.http = &httpPanel{split: 52}
	}
	p := d.http
	if p.done != nil {
		select {
		case result := <-p.done:
			p.cancel = nil
			p.done = nil
			p.reply = result
			if result.err != nil {
				p.err = result.err.Error()
				p.output = nil
			} else {
				p.err = ""
				p.showOutput()
			}
		default:
			c.After(50 * time.Millisecond)
		}
	}
	ui.Row(c).FillWidth().Height(36).Padding(0, 12).Gap(8).AlignItems(ui.Center).Children(func() {
		cursor, version := d.state.Cursor(), d.state.Buffer.Version()
		if !p.targetReady || p.targetCursor != cursor || p.targetVersion != version {
			req, label, err := requestAt(context.Background(), d.state.Buffer.String(), cursor)
			p.target = "HTTP 请求"
			if err == nil {
				p.target = label
				if label != req.Method+" "+req.URL.String() && !strings.HasPrefix(label, req.Method+" ") {
					p.target = req.Method + " · " + label
				}
			}
			p.targetCursor, p.targetVersion, p.targetReady = cursor, version, true
		}
		ui.Text(c, p.target).Bold().Grow(1).MinWidth(0).MaxLines(1).Tooltip(p.target)
		iconButton(c, "http-add", "plus", "追加请求", func() {
			end := d.state.Buffer.Len()
			d.state.Select(end, end)
			d.state.Insert("\n### 新请求\nGET http://localhost:8080/\n")
			a.focusEditor = true
		}).Disabled(a.git.mutating)
		iconButton(c, "http-curl", "files", "复制当前请求为 cURL", func() {
			req, _, err := requestAt(context.Background(), d.state.Buffer.String(), d.state.Cursor())
			if err != nil {
				a.report(err)
				return
			}
			command, err := curlCommand(req)
			if err != nil {
				a.report(err)
				return
			}
			golet.Clipboard.WriteText(command)
			a.notice = "已复制 cURL 命令"
		})

		if p.cancel != nil {
			muted(c, "请求中…").FontSize(11)
			iconButton(c, "http-stop", "stop", "取消请求", p.cancel)
		} else {
			iconButton(c, "http-run", "play", "发送光标所在的请求", func() { cursor := d.state.Cursor(); p.send(d, cursor, cursor) })
		}
	})
	edit := func() {
		d.state.ContextMenu = func(m *ui.Menu) {
			selection := d.state.Selection()
			start, end := selection.Start(), selection.End()
			_, _, err := requestInRange(context.Background(), d.state.Buffer.String(), start, end)
			if m.Item("run").Disabled(err != nil || p.cancel != nil || len(d.state.Selections()) > 1).Chosen() {
				p.send(d, start, end)
			}
		}
		a.textContent(c, d)
	}
	response := func() {
		ui.Column(c).Fill().MinWidth(0).Children(func() {
			ui.Row(c).FillWidth().Padding(4, 8).Gap(5).AlignItems(ui.Center).Children(func() {
				for _, v := range []struct {
					title string
					head  bool
				}{{"响应体", false}, {"响应头", true}} {
					item := v
					bg := ui.Transparent
					if p.headers == item.head {
						bg = a.theme.Sidebar
					}
					action(c, item.title, item.title, func() {
						p.headers = item.head
						if p.reply.status != "" {
							p.showOutput()
						}
					}).Background(bg)
				}
				ui.Box(c).Grow(1)
				if p.reply.status != "" {
					muted(c, p.reply.status).FontSize(11).Tooltip(p.reply.label)
				}
				if p.cancel == nil && (p.output != nil || p.err != "") {
					iconButton(c, "http-clear", "close", "清除响应", func() {
						p.reply = httpReply{}
						p.output = nil
						p.err = ""
					})
				}
			})
			if p.err != "" {
				ui.Text(c, p.err).TextColor(a.theme.Danger).Wrap().Padding(10)
			}
			if p.output != nil {
				desc := fmt.Sprintf("%s · %d ms · %d B", p.reply.label, p.reply.elapsed.Milliseconds(), p.reply.size)
				if p.reply.truncated {
					desc += " · 已截断"
				}
				muted(c, desc).FontSize(11).MaxLines(1).Tooltip(desc).Padding(4, 10)
				p.output.Theme = a.codeTheme
				p.output.Font = ui.Font{Family: a.prefs.EditorFont, Size: float32(a.prefs.EditorSize)}
				editor.View(c.Key("http-response"), p.output).Grow(1).FillWidth()
			} else if p.err == "" {
				ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Children(func() { muted(c, "发送请求后查看响应") })
			}
		})
	}
	w, _ := c.Size()
	if w < 1100 || a.ai.visible {
		ui.SplitVerticalPercent(c, &p.split, edit, response).Grow(1).FillWidth()
	} else {
		ui.SplitPercent(c, &p.split, edit, response).Grow(1).FillWidth()
	}
}

package main

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/markdown"
	"github.com/HyperparameterSI/golet/backend/ui/markdown/parser"
)

type markdownHeading struct {
	title, anchor string
	level, offset int
}
type markdownDocument struct {
	reading, outline, ready bool
	version                 uint64
	source                  string
	characters              int
	headings                []markdownHeading
	options                 markdown.Options
}

func (d *document) updateMarkdown() {
	m := &d.md
	version := d.state.Buffer.Version()
	if m.ready && m.version == version {
		return
	}
	m.ready, m.version = true, version
	m.source = d.state.Buffer.String()
	m.characters = utf8.RuneCountInString(m.source)
	m.headings = nil
	parsed := parser.Parse(m.source)
	for i := 0; i < parsed.Len(); i++ {
		b := parsed.Block(i)
		if b.Kind == parser.Heading {
			title := parser.PlainText(parsed.Inlines(i))
			m.headings = append(m.headings, markdownHeading{title, markdown.Slug(title), b.Level, b.Start})
		}
	}
}
func (a *app) markdownContent(c *ui.Context, d *document) {
	d.updateMarkdown()
	m := &d.md
	ui.Column(c).Grow(1).FillWidth().MinWidth(0).MinHeight(0).Children(func() {
		ui.Row(c).FillWidth().Height(34).Padding(0, 12).Gap(4).AlignItems(ui.Center).Children(func() {
			toolButton(c, "md-outline", "panel", "标题大纲", m.outline, func() { m.outline = !m.outline })
			iconButton(c, "md-format", "plus", "Markdown 格式", nil).Disabled(m.reading || a.git.mutating).Menu(func(menu *ui.Menu) {
				for _, name := range []string{"一级标题", "二级标题", "三级标题", "粗体", "斜体", "删除线", "行内代码", "代码块", "链接", "图片", "无序列表", "有序列表", "任务列表", "引用", "表格", "分隔线"} {
					if menu.Item(name).Chosen() {
						a.insertMarkdown(d, name)
					}
				}
			})
			muted(c, fmt.Sprintf("%d 字符 · %d 个标题", m.characters, len(m.headings))).Grow(1)
			toolButton(c, "md-source", "code", "源码模式", !d.preview && !m.reading, func() { d.preview = false; m.reading = false })
			toolButton(c, "md-split", "panel-right", "分栏预览", d.preview && !m.reading, func() { d.preview = true; m.reading = false })
			toolButton(c, "md-read", "file", "阅读模式", m.reading, func() { m.reading = true })
		})
		ui.Row(c).Grow(1).FillWidth().MinHeight(0).MinWidth(0).Children(func() {
			if m.outline {
				a.markdownOutline(c, d)
			}
			ui.Box(c).Grow(1).FillHeight().MinWidth(0).MinHeight(0).Children(func() {
				edit := func() { a.textContent(c, d) }
				preview := func() { a.markdownPreview(c, d) }
				if m.reading {
					preview()
				} else if d.preview {
					ui.SplitPercent(c, &a.split, edit, preview).Fill().MinWidth(0).MinHeight(0)
				} else {
					edit()
				}
			})
		})
	})
}
func (a *app) markdownOutline(c *ui.Context, d *document) {
	m := &d.md
	selected := -1
	for i, h := range m.headings {
		if h.offset <= d.state.Cursor() {
			selected = i
		}
	}
	t := c.Theme()
	ui.Column(c.Key("md-outline")).Width(224).Shrink(0).FillHeight().MinHeight(0).
		BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Children(func() {
		ui.Row(c).FillWidth().Height(38).Shrink(0).Padding(0, 16).AlignItems(ui.Center).Gap(8).Children(func() {
			muted(c, "文档大纲").FontSize(11).Grow(1)
			muted(c, fmt.Sprint(len(m.headings))).FontSize(11)
		})
		ui.Scroll(c.Key("md-outline-list")).Grow(1).FillWidth().MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().MinWidth(0).Padding(0, 8, 12, 8).Gap(3).Children(func() {
				if len(m.headings) == 0 {
					muted(c, "暂无标题").Padding(8)
				}
				for i, h := range m.headings {
					active := selected == i
					row := ui.ButtonBase(c.Key(fmt.Sprintf("heading-%d", i))).FillWidth().MinWidth(0).
						Height(30).MinHeight(30).Shrink(0).Padding(0, 8).Gap(8).Radius(5).
						AlignItems(ui.Center).Justify(ui.Start).Label(h.title).Tooltip(h.title).
						OnClick(func() {
							d.state.Select(h.offset, h.offset)
							d.state.ScrollTo(strings.Count(m.source[:h.offset], "\n"))
							m.options.Anchor = h.anchor
							m.options.AnchorVersion++
							a.focusEditor = !m.reading
						})
					fg := t.TextMuted
					if active {
						row.Background(t.SurfaceHover)
						fg = t.Text
					} else if row.Hovered() {
						row.Background(t.SurfaceHover)
						fg = t.Text
					}
					row.Children(func() {
						mark := ui.Box(c).Width(2).Height(14).Shrink(0).Radius(1)
						if active {
							mark.Background(t.Accent)
						}
						if h.level > 1 {
							ui.Box(c).Width(float32(min(h.level-1, 4) * 10)).Shrink(0)
						}
						label := ui.Text(c, h.title).FontSize(13).TextColor(fg).SingleLine().Ellipsis("…").Grow(1).Shrink(1).MinWidth(0)
						if active {
							label.Bold()
						}
					})
				}
			})
		})
	})
}

func (a *app) markdownPreview(c *ui.Context, d *document) {
	m := &d.md
	base := filepath.Dir(d.path)
	if d.path == "" {
		base = a.root
	}
	if m.options.Theme == nil {
		m.options.Theme = &markdown.Theme{FontSize: 16, LineHeight: 1.65}
	}
	m.options.Theme.CodeColors = a.codeTheme
	loadImage := markdown.FileImages(base)
	m.options.LoadImage = func(dest string) (ui.ImageSource, error) {
		u, err := url.Parse(dest)
		if err != nil {
			return nil, err
		}
		if u.Scheme != "" || u.Host != "" {
			return nil, fmt.Errorf("仅加载本地图片")
		}
		return loadImage(u.Path)
	}
	m.options.OnLink = func(dest string) { a.openMarkdownLink(c, d, dest) }
	ui.Scroll(c.Key(d.description() + "-markdown-preview")).Fill().MinWidth(0).Padding(28).Children(func() {
		ui.Row(c).FillWidth().Justify(ui.Center).Children(func() { markdown.View(c, m.source, &m.options).FillWidth().MaxWidth(800).MinWidth(0) })
	})
}
func (a *app) openMarkdownLink(c *ui.Context, d *document, dest string) {
	fmt.Println("MD LINK", dest)
	u, err := url.Parse(dest)
	if err != nil {
		a.report(err)
		return
	}
	if u.Scheme != "" {
		if ui.SafeToOpen(dest) {
			c.OpenURL(dest)
		}
		return
	}
	if u.Host != "" {
		return
	}
	target := d
	if u.Path != "" {
		base := filepath.Dir(d.path)
		if d.path == "" {
			base = a.root
		}
		path := u.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(base, filepath.FromSlash(path))
		}
		a.open(path)
		target = a.current()
		if target == nil || filepath.Clean(target.path) != filepath.Clean(path) {
			return
		}
		if target.markdown() {
			target.md.reading = d.md.reading
			target.preview = d.preview
		}
	}
	if target.markdown() && u.Fragment != "" {
		target.md.options.Anchor = u.Fragment
		target.md.options.AnchorVersion++
	}
}
func (a *app) insertMarkdown(d *document, kind string) {
	if a.git.mutating || d.md.reading {
		return
	}
	s := d.state.Selection()
	start, end := min(s.Anchor, s.Head), max(s.Anchor, s.Head)
	text := d.state.Buffer.Slice(start, end)
	before, after := "", ""
	switch kind {
	case "粗体":
		before, after = "**", "**"
	case "斜体":
		before, after = "*", "*"
	case "删除线":
		before, after = "~~", "~~"
	case "行内代码":
		before, after = "`", "`"
	case "代码块":
		before, after = "\n```\n", "\n```\n"
	case "链接":
		before, after = "[", "](https://example.com)"
	case "图片":
		before, after = "![", "](image.png)"
	case "表格":
		text = "\n| 列一 | 列二 |\n| --- | --- |\n| 内容 | 内容 |\n"
	case "分隔线":
		text = "\n---\n"
	default:
		prefixes := map[string]string{"一级标题": "# ", "二级标题": "## ", "三级标题": "### ", "无序列表": "- ", "有序列表": "1. ", "任务列表": "- [ ] ", "引用": "> "}
		prefix := prefixes[kind]
		if prefix == "" {
			return
		}
		source := d.state.Buffer.String()
		start = strings.LastIndex(source[:start], "\n") + 1
		text = d.state.Buffer.Slice(start, end)
		text = prefix + strings.ReplaceAll(text, "\n", "\n"+prefix)
	}
	if text == "" {
		text = "文字"
	}
	d.state.Select(start, end)
	d.state.Insert(before + text + after)
	d.state.Select(start+len(before), start+len(before)+len(text))
	a.focusEditor = true
}

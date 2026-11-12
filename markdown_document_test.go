package main

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/editor"
)

func TestMarkdownFormattingAndOutline(t *testing.T) {
	a := newApp("")
	d := newDocument("notes.md", "中文内容")
	d.state.Select(0, len("中文内容"))
	a.insertMarkdown(d, "粗体")
	if got := d.state.Buffer.String(); got != "**中文内容**" {
		t.Fatal(got)
	}
	d.state.Run(editor.Undo)
	if d.state.Buffer.String() != "中文内容" {
		t.Fatal("format undo failed")
	}
	d.state.Select(3, 3)
	a.insertMarkdown(d, "二级标题")
	if d.state.Buffer.String() != "## 中文内容" {
		t.Fatal("heading was inserted in middle of line")
	}
	d.state.Buffer.Set("# 开始\n\n```md\n# not heading\n```\n\n第二章\n---\n\n## **详情**\n")
	d.updateMarkdown()
	if len(d.md.headings) != 3 || d.md.headings[1].title != "第二章" || d.md.headings[2].title != "详情" {
		t.Fatalf("%+v", d.md.headings)
	}
	if d.md.characters != len([]rune(d.state.Buffer.String())) {
		t.Fatal("wrong character count")
	}
	d.md.reading = true
	before := d.state.Buffer.String()
	a.insertMarkdown(d, "粗体")
	if d.state.Buffer.String() != before {
		t.Fatal("reading mode changed source")
	}
}

func TestMarkdownWorkspaceExperience(t *testing.T) {
	root := t.TempDir()
	source := "# 项目文档\n\n原生 **Markdown** 阅读与编辑。\n\n## 使用说明\n\n- [x] 阅读文档\n- [ ] 完成设置\n\n| 功能 | 状态 |\n| --- | --- |\n| 本地文件 | 支持 |\n\n```go\nfmt.Println(\"Hello\")\n```\n\n> [!NOTE]\n> 修改仍保存为 Markdown。\n\n[下一页](next%20page.md#目标)\n"
	put(t, filepath.Join(root, "README.md"), source)
	put(t, filepath.Join(root, "next page.md"), "# 目标\n\n下一页内容")
	a := newApp(root)
	a.open(filepath.Join(root, "README.md"))
	d := a.current()
	v := ui.NewTester(a.view, 1200, 850)
	for _, label := range []string{"分栏预览", "标题大纲", "阅读模式"} {
		if err := v.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if !d.md.reading || !d.md.outline || !v.HasText("项目文档") {
		t.Fatal("missing reading mode")
	}
	v.Advance(200 * time.Millisecond)
	if os.Getenv("OMNICODE_MARKDOWN_SCREENSHOT") != "" {
		os.MkdirAll("artifacts", 0755)
		f, err := os.Create("artifacts/markdown-reading.png")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err = png.Encode(f, v.Image()); err != nil {
			t.Fatal(err)
		}
	}
	r, ok := v.Find("下一页")
	if !ok {
		t.Fatal("missing link")
	}
	v.ClickAt(float32(r.X+10), float32(r.Y+r.H/2))
	v.Frame()
	if a.current() == d || a.current().name != "next page.md" || a.current().md.options.Anchor != "目标" || !a.current().md.reading {
		t.Fatalf("relative link/anchor lost: notice=%s name=%s path=%s reading=%v anchor=%s", a.notice, a.current().name, a.current().path, a.current().md.reading, a.current().md.options.Anchor)
	}
	if err := v.Click("源码模式"); err != nil {
		t.Fatal(err)
	}
	if a.current().md.reading || a.current().preview {
		t.Fatal("source mode did not restore")
	}
	if !strings.Contains(d.state.Buffer.String(), "**Markdown**") {
		t.Fatal("mode switch changed source")
	}
}

func TestMarkdownLinkPreview(t *testing.T) {
	a := newApp("")
	d := newDocument("a.md", "[Jump](#heading)\n\n# heading")
	d.updateMarkdown()
	v := ui.NewTester(func(c *ui.Context) { a.markdownPreview(c, d) }, 800, 600)
	r, ok := v.Find("Jump")
	if !ok {
		t.Fatal("missing link")
	}
	v.ClickAt(float32(r.X+10), float32(r.Y+r.H/2))
	if d.md.options.Anchor != "heading" {
		t.Fatal("anchor not triggered")
	}
}

func TestMarkdownLongOutlineLayout(t *testing.T) {
	titles := []string{
		"Codex 是怎么把一句话，变成一次代码修改的？",
		"先把 Codex 想象成一个开发团队",
		"第一层：你看到的界面，是整个系统的入口",
		"第二层：App Server，让不同界面说同一种语言",
		"第三层：Core，负责把任务一轮轮推进",
		"工具系统：模型提出行动，程序负责动手",
		"为什么它能边工作，边向你汇报？",
		"权限与沙箱：同意执行，也有活动范围",
		"工作记录：它如何记住已经做过什么？",
		"如果只记住一张关系图",
	}
	a := newApp("")
	var src strings.Builder
	for i, title := range titles {
		level := "## "
		if i == 0 {
			level = "# "
		}
		src.WriteString(level + title + "\n\n正文说明。\n\n")
	}
	d := newDocument("codex架构公众号文章.md", src.String())
	a.docs = []*document{d}
	a.active = 0
	d.md.outline = true
	d.md.reading = true
	v := ui.NewTester(a.view, 1100, 800)
	previous := float32(0)
	for _, title := range titles {
		r, ok := v.Find(title)
		if !ok || r.H > 31 || r.W > 224 {
			t.Fatalf("outline row overflow: %s %+v", title, r)
		}
		if previous != 0 && r.Y < previous {
			t.Fatal("outline rows overlap")
		}
		previous = r.Y + r.H
	}
	if err := v.Click(titles[3]); err != nil {
		t.Fatal(err)
	}
	if d.state.Cursor() != d.md.headings[3].offset {
		t.Fatal("outline navigation failed")
	}
	v.Advance(200 * time.Millisecond)
	if os.Getenv("OMNICODE_MARKDOWN_SCREENSHOT") != "" {
		os.MkdirAll("artifacts", 0755)
		f, err := os.Create("artifacts/markdown-outline.png")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err = png.Encode(f, v.Image()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMarkdownSearchFromReading(t *testing.T) {
	a := newApp("")
	d := newDocument("search.md", "# 搜索\n\n中文 target\n\n第二处 target\n")
	d.md.reading = true
	v := ui.NewTester(func(c *ui.Context) { a.markdownContent(c, d) }, 1000, 650)
	if err := v.Click("搜索文档 (⌘F)"); err != nil {
		t.Fatal(err)
	}
	if d.md.reading || !d.preview {
		t.Fatal("search must expose source alongside preview")
	}
	d.state.Find("target")
	v.Frame()
	if n, _ := d.state.FindMatches(); n != 2 {
		t.Fatalf("matches = %d", n)
	}
	d.state.Run(editor.FindNext)
	v.Frame()
	sel := d.state.Selection()
	if got := d.state.Buffer.Slice(min(sel.Anchor, sel.Head), max(sel.Anchor, sel.Head)); got != "target" {
		t.Fatalf("selected %q", got)
	}
}

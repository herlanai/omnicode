package main

import (
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/HyperparameterSI/golet/backend/ui"
)

func TestReviewMetadataCollapsesAndScrollsWithResult(t *testing.T) {
	a := newApp("")
	r := &gitReviewUI{status: "审查完成", meta: "审查范围 · 40 个文件\n已跳过：" + strings.Repeat("very/long/path/file.go（超过审查大小限制）、", 180), output: "## 审查结论\n\n结果末尾", scope: "all"}
	v := ui.NewTester(func(c *ui.Context) { c.SetTheme(a.theme); a.reviewContent(c, r, "test") }, 800, 600)
	if r.metaExpanded {
		t.Fatal("details should start collapsed")
	}
	result, ok := v.Find("结果末尾")
	if !ok || result.Y >= 600 {
		t.Fatal("collapsed metadata hides result")
	}
	if err := v.Click("审查范围详情"); err != nil {
		t.Fatal(err)
	}
	if !r.metaExpanded {
		t.Fatal("details did not expand")
	}
	if os.Getenv("OMNICODE_REVIEW_SCREENSHOT") != "" {
		os.MkdirAll("artifacts", 0755)
		f, err := os.Create("artifacts/review-expanded.png")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, v.Image()); err != nil {
			t.Fatal(err)
		}
	}
	// Scroll while pointing at the metadata, not the result below it.
	meta, ok := v.Find("审查范围详情")
	if !ok || meta.H == 0 {
		t.Fatal("metadata header not visible")
	}
	v.Scroll(meta.X+meta.W/2, meta.Y+meta.H/2, 0, 20000)
	v.Frame()
	result, ok = v.Find("结果末尾")
	if !ok || result.Y < 40 || result.Y+result.H > 600 || result.H == 0 {
		t.Fatalf("result unreachable after scroll: %+v, found %v", result, ok)
	}
	if header, ok := v.Find("审查范围详情"); ok && header.H > 0 {
		t.Fatal("metadata did not scroll together with result")
	}
	if os.Getenv("OMNICODE_REVIEW_SCREENSHOT") != "" {
		f, err := os.Create("artifacts/review-bottom.png")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, v.Image()); err != nil {
			t.Fatal(err)
		}
	}
	v.Scroll(400, 350, 0, -20000)
	v.Frame()
	if err := v.Click("审查范围详情"); err != nil {
		t.Fatal(err)
	}
	if r.metaExpanded {
		t.Fatal("details did not collapse")
	}
}

func TestReviewScrollbarVisibleAndDraggable(t *testing.T) {
	a := newApp("")
	theme := *a.theme
	theme.Scrollbar = ui.RGB(240, 20, 30)
	r := &gitReviewUI{output: strings.Repeat("Paragraph in the review result.\n\n", 100), scope: "all"}
	v := ui.NewTester(func(c *ui.Context) { c.SetTheme(&theme); a.reviewContent(c, r, "test") }, 800, 600)
	v.Move(10, 10) // Outside the scrolling content: the thumb must still be visible.
	v.Frame()
	img := v.Image()
	x, y := -1, -1
	for py := 40; py < 600 && x < 0; py++ {
		for px := 788; px < 800; px++ {
			color := img.RGBAAt(px, py)
			if color.R > 180 && color.G < 90 && color.B < 90 {
				x, y = px, py
				break
			}
		}
	}
	if x < 0 {
		t.Fatal("no visible scrollbar with pointer outside content")
	}
	x = 795
	v.Press(float32(x), float32(y+10))
	v.Move(float32(x), 590)
	v.Release(float32(x), 590)
	v.Frame()
	if button, ok := v.Find("开始审查"); ok && button.H > 0 && button.Y >= 40 {
		t.Fatalf("dragging scrollbar did not scroll content: %+v", button)
	}
}

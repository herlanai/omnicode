package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/HyperparameterSI/golet/backend/plugins/terminal"
	"github.com/HyperparameterSI/golet/backend/ui"
)

func samplePDF() string {
	stream := "1 0 0 rg 0 150 200 150 re f\n0 0 1 rg 0 0 200 150 re f\n"
	objs := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R 5 0 R] /Count 2 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 300] /Contents 4 0 R >>", fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(stream), stream), "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Contents 4 0 R >>"}
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, obj := range objs {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.String()
}
func TestImageViewerAndReadOnly(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "test.png")
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	img := image.NewRGBA(image.Rect(0, 0, 20, 10))
	img.Set(2, 2, color.White)
	if e = png.Encode(f, img); e != nil {
		t.Fatal(e)
	}
	f.Close()
	d, e := loadDocument(path)
	if e != nil {
		t.Fatal(e)
	}
	if d.kind != imageFile || d.editable() || d.media.width != 20 {
		t.Fatal("incorrect image routing")
	}
	if e = d.save(path); e == nil {
		t.Fatal("binary save allowed")
	}
	a := newApp(root)
	a.open(path)
	tester := ui.NewTester(a.view, 1200, 780)
	if !tester.HasText("20 × 10") {
		t.Fatal("image view missing")
	}
	if e = tester.Click("100%"); e != nil {
		t.Fatal(e)
	}
	tester.SetSize(800, 520)
	svg := filepath.Join(root, "vector.svg")
	put(t, svg, `<svg width="40" height="20" xmlns="http://www.w3.org/2000/svg"><rect width="40" height="20" fill="red"/></svg>`)
	if _, e = loadDocument(svg); e != nil {
		t.Fatal(e)
	}
	put(t, path, "not an image")
	if _, e = loadDocument(path); e == nil {
		t.Fatal("accepted corrupt image")
	}
}
func TestPDFViewer(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Core Graphics is macOS-only")
	}
	root := t.TempDir()
	path := filepath.Join(root, "sample.pdf")
	put(t, path, samplePDF())
	img, count, e := renderPDF(path, 1)
	if e != nil {
		t.Fatal(e)
	}
	if count != 2 || img.Bounds().Dx() != 400 || img.Bounds().Dy() != 600 {
		t.Fatal("incorrect page dimensions")
	}
	top := img.RGBAAt(100, 100)
	bottom := img.RGBAAt(100, 500)
	if top.R < 200 || top.B > 20 || bottom.B < 200 {
		t.Fatalf("incorrect orientation/colors top=%v bottom=%v", top, bottom)
	}
	a := newApp(root)
	a.open(path)
	if a.current() == nil {
		t.Fatal(a.notice)
	}
	tester := ui.NewTester(a.view, 1200, 780)
	if e = tester.Click("下一页"); e != nil {
		t.Fatal(e)
	}
	if a.current().media.page != 2 || a.current().media.width != 600 {
		t.Fatal("page navigation failed")
	}
	if _, _, e = renderPDF(path, 3); e == nil {
		t.Fatal("accepted invalid page")
	}
	if dir := os.Getenv("OMNICODE_VIEWER_ARTIFACTS"); dir != "" {
		os.MkdirAll(dir, 0755)
		out, e := os.Create(filepath.Join(dir, "pdf.png"))
		if e != nil {
			t.Fatal(e)
		}
		defer out.Close()
		png.Encode(out, tester.Image())
	}
}
func TestTerminalSession(t *testing.T) {
	if os.Getenv("OMNICODE_TERMINAL_TEST") == "" {
		t.Skip("requires native PTY; enable OMNICODE_TERMINAL_TEST=1")
	}
	session, e := terminal.New(terminal.Options{Command: []string{"/bin/sh", "-c", "printf 'omnicode-terminal-ok'; exit 0"}, Dir: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close()
	a := newApp("")
	a.terminal = terminalPanel{session: session, visible: true, size: 65}
	tester := ui.NewTester(a.view, 1200, 780)
	select {
	case <-session.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("terminal did not exit")
	}
	if session.ExitCode() != 0 {
		t.Fatal("shell failed")
	}
	tester.Advance(time.Second)
	if e := tester.Click("移到右侧"); e != nil {
		t.Fatal(e)
	}
	if !a.terminal.right {
		t.Fatal("terminal docking failed")
	}
	if dir := os.Getenv("OMNICODE_VIEWER_ARTIFACTS"); dir != "" {
		os.MkdirAll(dir, 0755)
		out, e := os.Create(filepath.Join(dir, "terminal.png"))
		if e != nil {
			t.Fatal(e)
		}
		defer out.Close()
		png.Encode(out, tester.Image())
	}
}

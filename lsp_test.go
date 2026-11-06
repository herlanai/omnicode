package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/editor"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLSPPositions(t *testing.T) {
	s := "中文😀x\nabc"
	for _, offset := range []int{0, 3, 6, 10, 11, 12, 13, 15} {
		p := lspPos(s, offset)
		if got := lspOffset(s, p); got != offset {
			t.Fatalf("%d -> %+v -> %d", offset, p, got)
		}
	}
	if lspPos(s, 10).Character != 4 {
		t.Fatal("UTF16 astral character")
	}
	path := "/tmp/中文 #test.go"
	if lspPath(lspURI(path)) != path {
		t.Fatal("URI roundtrip")
	}
}
func TestLSPFrames(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":2,"result":"中文"}`
	p, e := readLSP(bufio.NewReader(strings.NewReader(fmt.Sprintf("Content-Length: %d\r\nContent-Type: application/vscode-jsonrpc\r\n\r\n%s", len(body), body))))
	if e != nil || string(p.Result) != `"中文"` {
		t.Fatalf("%+v %v", p, e)
	}
	if _, e = readLSP(bufio.NewReader(strings.NewReader("Content-Length: 999999999\r\n\r\n"))); e == nil {
		t.Fatal("unbounded frame")
	}
}
func TestLSPCompletionEdits(t *testing.T) {
	j := languageJob{target: languageSnapshot{text: "fmt.Pr"}, offset: 6, start: 4}
	raw := json.RawMessage(`[{"label":"Print","textEdit":{"range":{"start":{"line":0,"character":4},"end":{"line":0,"character":6}},"newText":"Print"}},{"label":"bad","additionalTextEdits":[{}]},{"label":"snippet","insertTextFormat":2,"insertText":"${UNKNOWN}"},{"label":"wrong","textEdit":{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":6}},"newText":"oops"}}]`)
	items := lspCompletions(raw, j)
	if len(items) != 1 || items[0].Insert != "Print" {
		t.Fatalf("%+v", items)
	}
}
func TestBundledLanguageServers(t *testing.T) {
	if os.Getenv("OMNICODE_LSP_TEST") != "1" {
		t.Skip("opt-in real language servers")
	}
	for _, tc := range []struct{ kind, name, text, needle, want string }{
		{"go", "main.go", "package main\nimport \"fmt\"\nfunc main(){fmt.Pr}\n", "fmt.Pr", "Println"},
		{"typescript", "main.ts", "const person = { name: 'Omni', age: 2 };\nperson.na", "person.na", "name"},
		{"html", "index.html", "<div cl", "cl", "class"},
		{"css", "style.css", "body { col", "col", "color"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			root := t.TempDir()
			_ = os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n\ngo 1.27.0\n"), 0600)
			path := filepath.Join(root, tc.name)
			_ = os.WriteFile(path, []byte(tc.text), 0600)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			rpc, e := startLSP(ctx, tc.kind, root, nil)
			if e != nil {
				t.Fatal(e)
			}
			_, lang := lspLanguage(path)
			e = rpc.notification("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": lspURI(path), "languageId": lang, "version": 1, "text": tc.text}})
			if e != nil {
				t.Fatal(e)
			}
			offset := strings.Index(tc.text, tc.needle) + len(tc.needle)
			start := offset
			for start > 0 && ((tc.text[start-1] >= 'a' && tc.text[start-1] <= 'z') || (tc.text[start-1] >= 'A' && tc.text[start-1] <= 'Z')) {
				start--
			}
			var raw json.RawMessage
			for tries := 0; tries < 10; tries++ {
				e = rpc.call(ctx, "textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": lspURI(path)}, "position": lspPos(tc.text, offset)}, &raw)
				if e == nil {
					for _, it := range lspCompletions(raw, languageJob{target: languageSnapshot{text: tc.text}, offset: offset, start: start}) {
						if it.Label == tc.want || strings.HasPrefix(it.Label, tc.want+"(") {
							return
						}
					}
				}
				time.Sleep(200 * time.Millisecond)
			}
			t.Fatalf("missing %s: err=%v result=%.1000s", tc.want, e, string(raw))
		})
	}
}

func TestLSPSnippets(t *testing.T) {
	for _, v := range []struct {
		input, text string
		caret       int
	}{{`class="$1"`, `class=""`, 7}, {`color: $0;`, `color: ;`, 7}, {`${1:hello}($0)`, `hello()`, 0}, {`${1|red,blue|}`, `red`, 0}} {
		text, caret, ok := plainSnippet(v.input)
		if !ok || text != v.text || caret != v.caret {
			t.Fatalf("%q -> %q %d %v", v.input, text, caret, ok)
		}
	}
	if _, _, ok := plainSnippet(`${1/foo/bar/}`); ok {
		t.Fatal("transform must not be inserted")
	}
}
func TestNativeLSPCompletion(t *testing.T) {
	d := newDocument("main.go", "fmt.Pr")
	a := newApp("")
	a.docs = []*document{d}
	a.active = 0
	tt := ui.NewTester(func(c *ui.Context) { editor.View(c, d.state).Fill().AutoFocus() }, 640, 400)
	tt.Frame()
	d.state.Select(6, 6)
	tt.Frame()
	w := &languageWorker{serial: 1}
	j := languageJob{target: languageSnapshot{doc: d, text: "fmt.Pr", version: d.state.Buffer.Version()}, offset: 6, start: 4, serial: 1, method: "textDocument/completion"}
	a.applyIntelligence(intelligenceEvent{worker: w, job: j, result: json.RawMessage(`[{"label":"Println","insertText":"Println"}]`)})
	tt.Frame()
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if got := d.state.Buffer.String(); got != "fmt.Println" {
		t.Fatalf("native insertion = %q", got)
	}
	// A response from the old buffer must never reopen a stale list.
	a.applyIntelligence(intelligenceEvent{worker: w, job: j, result: json.RawMessage(`[{"label":"BAD"}]`)})
	tt.Frame()
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if strings.Contains(d.state.Buffer.String(), "BAD") {
		t.Fatal("stale result inserted")
	}
}
func TestLSPUnsavedNavigationAndDiagnostics(t *testing.T) {
	if os.Getenv("OMNICODE_LSP_TEST") != "1" {
		t.Skip("opt-in")
	}
	root := t.TempDir()
	path := filepath.Join(root, "main.ts")
	text := "const person = { name: 'Omni' };\nperson.name;"
	_ = os.WriteFile(path, []byte(text), 0600)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	diagnostics := make(chan json.RawMessage, 32)
	rpc, e := startLSP(ctx, "typescript", root, func(method string, p json.RawMessage) {
		if method == "textDocument/publishDiagnostics" {
			select {
			case diagnostics <- p:
			default:
			}
		}
	})
	if e != nil {
		t.Fatal(e)
	}
	doc := map[string]any{"uri": lspURI(path)}
	_ = rpc.notification("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": lspURI(path), "languageId": "typescript", "version": 1, "text": text}})
	var hover struct{ Contents json.RawMessage }
	e = rpc.call(ctx, "textDocument/hover", map[string]any{"textDocument": doc, "position": lspPos(text, strings.LastIndex(text, "name"))}, &hover)
	if e != nil || !strings.Contains(lspDocumentation(hover.Contents), "string") {
		t.Fatalf("hover %s %v", hover.Contents, e)
	}
	var definition json.RawMessage
	e = rpc.call(ctx, "textDocument/definition", map[string]any{"textDocument": doc, "position": lspPos(text, strings.LastIndex(text, "name"))}, &definition)
	if e != nil || !strings.Contains(string(definition), "main.ts") {
		t.Fatalf("definition %s %v", definition, e)
	}
	updated := "const person = { surname: 'Omni' };\nperson.sur"
	_ = rpc.notification("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": lspURI(path), "version": 2}, "contentChanges": []any{map[string]any{"text": updated}}})
	var result json.RawMessage
	e = rpc.call(ctx, "textDocument/completion", map[string]any{"textDocument": doc, "position": lspPos(updated, len(updated))}, &result)
	if e != nil || !strings.Contains(string(result), "surname") {
		t.Fatalf("unsaved completion %.300s %v", result, e)
	}
	broken := "const age: number = 'bad';"
	_ = rpc.notification("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": lspURI(path), "version": 3}, "contentChanges": []any{map[string]any{"text": broken}}})
	for {
		select {
		case p := <-diagnostics:
			var v struct {
				Version     *int
				Diagnostics []json.RawMessage
			}
			_ = json.Unmarshal(p, &v)
			if len(v.Diagnostics) > 0 {

				return
			}
		case <-ctx.Done():
			t.Fatal("no diagnostics")
			return
		}
	}
}

func TestNativeSnippetCaret(t *testing.T) {
	d := newDocument("index.html", "<div cl")
	a := newApp("")
	a.docs = []*document{d}
	a.active = 0
	tt := ui.NewTester(func(c *ui.Context) { editor.View(c, d.state).Fill().AutoFocus() }, 640, 400)
	tt.Frame()
	d.state.Select(7, 7)
	tt.Frame()
	w := &languageWorker{serial: 1}
	j := languageJob{target: languageSnapshot{doc: d, text: "<div cl", version: d.state.Buffer.Version()}, offset: 7, start: 5, serial: 1, method: "textDocument/completion"}
	a.applyIntelligence(intelligenceEvent{worker: w, job: j, result: json.RawMessage(`[{"label":"class","insertTextFormat":2,"insertText":"class=\"$1\""}]`)})
	tt.Frame()
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	a.positionAcceptedCompletion()
	if d.state.Buffer.String() != `<div class=""` || d.state.Cursor() != 12 {
		t.Fatalf("text %q caret %d", d.state.Buffer.String(), d.state.Cursor())
	}
}
func TestLSPJSONDiagnostics(t *testing.T) {
	if os.Getenv("OMNICODE_LSP_TEST") != "1" {
		t.Skip("opt-in")
	}
	root := t.TempDir()
	path := filepath.Join(root, "data.json")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	events := make(chan json.RawMessage, 8)
	rpc, e := startLSP(ctx, "json", root, func(method string, raw json.RawMessage) {
		if method == "textDocument/publishDiagnostics" {
			select {
			case events <- raw:
			default:
			}
		}
	})
	if e != nil {
		t.Fatal(e)
	}
	_ = rpc.notification("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": lspURI(path), "languageId": "json", "version": 1, "text": "{broken}"}})
	for {
		select {
		case raw := <-events:
			var p struct{ Diagnostics []json.RawMessage }
			_ = json.Unmarshal(raw, &p)
			if len(p.Diagnostics) > 0 {
				return
			}
		case <-ctx.Done():
			t.Fatal("JSON diagnostics missing")
			return
		}
	}
}

func TestMethodDefinitionNavigation(t *testing.T) {
	if os.Getenv("OMNICODE_LSP_TEST") != "1" {
		t.Skip("requires bundled gopls")
	}
	a := newApp(t.TempDir())
	put(t, filepath.Join(a.root, "go.mod"), "module jump.test\n\ngo 1.22\n")
	put(t, filepath.Join(a.root, "other.go"), "package jump\ntype service struct{}\nfunc (service) Remote() {}\n")
	source := "package jump\nfunc (service) Local() {}\nfunc call(s service) { s.Local(); s.Remote() }\n"
	path := filepath.Join(a.root, "main.go")
	put(t, path, source)
	a.open(path)
	d := a.current()
	worker := &languageWorker{kind: "go", jobs: make(chan languageJob, 1), sent: map[string]languageSnapshot{}}
	a.intelligence.servers = map[string]*languageWorker{"go": worker}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	worker.cancel = cancel
	events := make(chan intelligenceEvent, 128)
	go worker.run(ctx, a.root, events)
	d.state.OnDefinition = func(off int) { a.requestIntelligence(worker, d, definitionMethod, off, off) }
	view := ui.NewTester(func(c *ui.Context) { a.textContent(c, d) }, 800, 550)
	for _, tc := range []struct{ name, file string }{{"Local", "main.go"}, {"Remote", "other.go"}} {
		off := strings.LastIndex(source, tc.name)
		d.state.Select(off, off)
		view.Frame()
		view.Key(0, ui.KeyF12)
		if worker.navigation == nil {
			t.Fatal("F12 did not request a definition")
		}
		a.requestIntelligence(worker, d, "textDocument/hover", off, off)
		for worker.navigation != nil {
			select {
			case e := <-events:
				a.applyIntelligence(e)
				if worker.failed {
					t.Fatal(a.notice)
				}
			case <-ctx.Done():
				t.Fatal("definition timed out", a.notice)
			}
		}
		got := a.current()
		if filepath.Base(got.path) != tc.file || got.state.SelectedText() != tc.name {
			t.Fatalf("%s: %s selected %q (%s)", tc.name, got.path, got.state.SelectedText(), a.notice)
		}
	}
}

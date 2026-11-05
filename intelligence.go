package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/editor"
)

type intelligence struct {
	accepted map[*document]completionInsertion
	root     string
	servers  map[string]*languageWorker
	events   chan intelligenceEvent
}
type languageSnapshot struct {
	path, text, language string
	version              uint64
	doc                  *document
}
type languageJob struct {
	docs          []languageSnapshot
	target        languageSnapshot
	method        string
	offset, start int
	serial        uint64
}
type intelligenceEvent struct {
	worker     *languageWorker
	job        languageJob
	result     json.RawMessage
	diagnostic bool
	err        error
}
type languageWorker struct {
	requestMu     sync.Mutex
	requestCancel context.CancelFunc
	kind          string
	jobs          chan languageJob
	cancel        context.CancelFunc
	sent          map[string]languageSnapshot
	serial        uint64
	failed        bool
}

func (a *app) closeIntelligence() {
	for _, w := range a.intelligence.servers {
		w.cancel()
	}
	a.intelligence = intelligence{}
}
func (a *app) intelligenceSnapshots(kind string) []languageSnapshot {
	var docs []languageSnapshot
	for _, d := range a.docs {
		server, lang := lspLanguage(d.path)
		if server != kind || d.state == nil || d.path == "" {
			continue
		}
		version := d.state.Buffer.Version()
		if w := a.intelligence.servers[kind]; w != nil {
			if old, ok := w.sent[d.path]; ok && old.doc == d && old.version == version {
				docs = append(docs, old)
				continue
			}
		}
		docs = append(docs, languageSnapshot{d.path, d.state.Buffer.String(), lang, version, d})
	}
	return docs
}
func (w *languageWorker) submit(j languageJob) {
	w.requestMu.Lock()
	if w.requestCancel != nil {
		w.requestCancel()
	}
	w.requestMu.Unlock()
	select {
	case w.jobs <- j:
	default:
		select {
		case <-w.jobs:
		default:
		}
		w.jobs <- j
	}
}
func (a *app) updateIntelligence(c *ui.Context) {
	a.positionAcceptedCompletion()
	if a.intelligence.root != a.root {
		a.closeIntelligence()
	}
	// Native window only: screenshots and UI unit tests should not launch servers.
	if a.win == nil {
		return
	}
	if a.intelligence.servers == nil {
		a.intelligence = intelligence{root: a.root, servers: map[string]*languageWorker{}, events: make(chan intelligenceEvent, 128)}
	}
	for _, d := range a.docs {
		kind, _ := lspLanguage(d.path)
		if kind == "" || d.state == nil {
			continue
		}
		w := a.intelligence.servers[kind]
		if w == nil {
			ctx, cancel := context.WithCancel(context.Background())
			w = &languageWorker{kind: kind, jobs: make(chan languageJob, 1), cancel: cancel, sent: map[string]languageSnapshot{}}
			a.intelligence.servers[kind] = w
			root := a.root
			if root == "" {
				root = filepath.Dir(d.path)
			}
			go w.run(ctx, root, a.intelligence.events)
		}
		doc := d
		worker := w
		d.state.OnCompletion = func(r editor.CompletionRequest) {
			a.requestIntelligence(worker, doc, "textDocument/completion", r.Offset, r.Offset-len(r.Prefix))
		}
		d.state.OnHover = func(r editor.HoverRequest) {
			a.requestIntelligence(worker, doc, "textDocument/hover", r.Offset, r.Offset)
		}
		d.state.OnDefinition = func(offset int) { a.requestIntelligence(worker, doc, "textDocument/definition", offset, offset) }
	}
	for _, w := range a.intelligence.servers {
		if w.failed {
			continue
		}
		docs := a.intelligenceSnapshots(w.kind)
		changed := len(docs) != len(w.sent)
		for _, d := range docs {
			if old := w.sent[d.path]; old.version != d.version || old.doc != d.doc {
				changed = true
			}
		}
		if changed {
			w.sent = map[string]languageSnapshot{}
			for _, d := range docs {
				w.sent[d.path] = d
			}
			w.submit(languageJob{docs: docs})
		}
	}
	for {
		select {
		case e := <-a.intelligence.events:
			a.applyIntelligence(e)
		default:
			if len(a.intelligence.servers) > 0 {
				c.After(80 * time.Millisecond)
			}
			return
		}
	}
}
func (a *app) requestIntelligence(w *languageWorker, d *document, method string, offset, start int) {
	if w.failed {
		return
	}
	w.serial++
	docs := a.intelligenceSnapshots(w.kind)
	w.sent = map[string]languageSnapshot{}
	for _, s := range docs {
		w.sent[s.path] = s
	}
	for _, s := range docs {
		if s.doc == d {
			w.submit(languageJob{docs: docs, target: s, method: method, offset: offset, start: start, serial: w.serial})
			return
		}
	}
}
func (w *languageWorker) run(ctx context.Context, root string, events chan<- intelligenceEvent) {
	emit := func(e intelligenceEvent) {
		e.worker = w
		select {
		case events <- e:
		case <-ctx.Done():
		}
	}
	var syncedMu sync.Mutex
	synced := map[string]languageSnapshot{}
	rpc, e := startLSP(ctx, w.kind, root, func(method string, b json.RawMessage) {
		if method == "textDocument/publishDiagnostics" {
			var header struct {
				URI string `json:"uri"`
			}
			_ = json.Unmarshal(b, &header)
			syncedMu.Lock()
			snapshot, ok := synced[lspPath(header.URI)]
			syncedMu.Unlock()
			if ok {
				emit(intelligenceEvent{result: b, diagnostic: true, job: languageJob{target: snapshot}})
			}
		}
	})
	if e != nil {
		emit(intelligenceEvent{err: e})
		w.cancel()
		return
	}
	defer w.cancel()
	opened := map[string]languageSnapshot{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-rpc.done:
			emit(intelligenceEvent{err: fmt.Errorf("语言服务已停止，重新打开项目后重试")})
			return
		case j := <-w.jobs:
			present := map[string]bool{}
			for _, s := range j.docs {
				present[s.path] = true
				syncedMu.Lock()
				synced[s.path] = s
				syncedMu.Unlock()
				old, ok := opened[s.path]
				if ok && old.doc != s.doc {
					_ = rpc.notification("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": lspURI(s.path)}})
					ok = false
				}
				if !ok {
					_ = rpc.notification("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": lspURI(s.path), "languageId": s.language, "version": s.version + 1, "text": s.text}})
				} else if s.version != old.version {
					_ = rpc.notification("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": lspURI(s.path), "version": s.version + 1}, "contentChanges": []any{map[string]any{"text": s.text}}})
				}
				opened[s.path] = s
			}
			for path := range opened {
				if !present[path] {
					_ = rpc.notification("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": lspURI(path)}})
					delete(opened, path)
					syncedMu.Lock()
					delete(synced, path)
					syncedMu.Unlock()
				}
			}
			if j.method == "" {
				continue
			}
			reqCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			w.requestMu.Lock()
			w.requestCancel = cancel
			w.requestMu.Unlock()
			var result json.RawMessage
			err := rpc.call(reqCtx, j.method, map[string]any{"textDocument": map[string]any{"uri": lspURI(j.target.path)}, "position": lspPos(j.target.text, j.offset)}, &result)
			cancel()
			w.requestMu.Lock()
			w.requestCancel = nil
			w.requestMu.Unlock()
			if err == nil {
				emit(intelligenceEvent{job: j, result: result})
			}
		}
	}
}
func lspDocumentation(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var m struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &m) == nil && m.Value != "" {
		return m.Value
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) == nil {
		var result []string
		for _, p := range parts {
			if s := lspDocumentation(p); s != "" {
				result = append(result, s)
			}
		}
		return strings.Join(result, "\n\n")
	}
	return ""
}

type lspCompletionItem struct {
	Label            string          `json:"label"`
	Detail           string          `json:"detail"`
	Kind             int             `json:"kind"`
	InsertText       string          `json:"insertText"`
	InsertTextFormat int             `json:"insertTextFormat"`
	FilterText       string          `json:"filterText"`
	Documentation    json.RawMessage `json:"documentation"`
	TextEdit         *struct {
		Range   *lspRange `json:"range"`
		Insert  *lspRange `json:"insert"`
		NewText string    `json:"newText"`
	} `json:"textEdit"`
	AdditionalTextEdits []json.RawMessage `json:"additionalTextEdits"`
}

func lspCompletions(raw json.RawMessage, j languageJob) []editor.Completion {
	var items []lspCompletionItem
	if json.Unmarshal(raw, &items) != nil {
		var list struct {
			Items []lspCompletionItem `json:"items"`
		}
		_ = json.Unmarshal(raw, &list)
		items = list.Items
	}
	var result []editor.Completion
	for _, it := range items {
		// Native Golet currently inserts a prefix only. Never silently drop import edits
		// or replace an unrelated range; those items require an atomic edit API.
		if len(it.AdditionalTextEdits) > 0 {
			continue
		}
		insert := it.InsertText
		if it.TextEdit != nil {
			r := it.TextEdit.Range
			if r == nil {
				r = it.TextEdit.Insert
			}
			if r == nil || lspOffset(j.target.text, r.Start) != j.start || lspOffset(j.target.text, r.End) != j.offset {
				continue
			}
			insert = it.TextEdit.NewText
		}
		if it.InsertTextFormat == 2 {
			var ok bool
			insert, _, ok = plainSnippet(insert)
			if !ok {
				continue
			}
		}
		kind := editor.CompletionText
		switch it.Kind {
		case 2:
			kind = editor.CompletionMethod
		case 3, 4:
			kind = editor.CompletionFunction
		case 5, 10:
			kind = editor.CompletionField
		case 6:
			kind = editor.CompletionVariable
		case 7, 8, 13, 22, 25:
			kind = editor.CompletionType
		case 9:
			kind = editor.CompletionModule
		case 14:
			kind = editor.CompletionKeyword
		case 21:
			kind = editor.CompletionConstant
		}
		result = append(result, editor.Completion{Label: it.Label, Detail: it.Detail, Kind: kind, Insert: insert, Documentation: lspDocumentation(it.Documentation), FilterText: it.FilterText})
		if len(result) == 2000 {
			break
		}
	}
	return result
}
func (a *app) applyIntelligence(e intelligenceEvent) {
	if e.err != nil {
		e.worker.failed = true
		a.notice = "智能提示 · " + e.worker.kind + ": " + e.err.Error()
		return
	}
	if e.diagnostic {
		var p struct {
			URI         string  `json:"uri"`
			Version     *uint64 `json:"version"`
			Diagnostics []struct {
				Range    lspRange `json:"range"`
				Severity int      `json:"severity"`
				Message  string   `json:"message"`
				Source   string   `json:"source"`
			} `json:"diagnostics"`
		}
		if json.Unmarshal(e.result, &p) != nil {
			return
		}
		if p.Version == nil {
			version := e.job.target.version + 1
			p.Version = &version
		}
		for _, d := range a.docs {
			if d != e.job.target.doc || d.path != lspPath(p.URI) || d.state == nil || d.state.Buffer.Version()+1 != *p.Version {
				continue
			}
			text := d.state.Buffer.String()
			var ds []editor.Diagnostic
			for _, v := range p.Diagnostics {
				severity := max(0, min(3, v.Severity-1))
				ds = append(ds, editor.Diagnostic{Start: lspOffset(text, v.Range.Start), End: lspOffset(text, v.Range.End), Severity: editor.Severity(severity), Message: v.Message, Source: v.Source})
			}
			d.state.SetDiagnostics(d.state.Buffer.Version(), ds)
		}
		return
	}
	j := e.job
	d := j.target.doc
	if d == nil || a.current() != d || d.state.Buffer.Version() != j.target.version || e.worker.serial != j.serial {
		return
	}
	switch j.method {
	case "textDocument/completion":
		if d.state.Cursor() == j.offset {
			items := lspCompletions(e.result, j)
			if a.intelligence.accepted == nil {
				a.intelligence.accepted = map[*document]completionInsertion{}
			}
			a.intelligence.accepted[d] = completionInsertion{job: j, carets: lspSnippetCarets(e.result)}
			d.state.ShowCompletions(j.start, items)
		}
	case "textDocument/hover":
		var h struct {
			Contents json.RawMessage `json:"contents"`
			Range    *lspRange       `json:"range"`
		}
		if json.Unmarshal(e.result, &h) == nil {
			text := lspDocumentation(h.Contents)
			if text != "" {
				start, end := j.offset, j.offset
				if h.Range != nil {
					start = lspOffset(j.target.text, h.Range.Start)
					end = lspOffset(j.target.text, h.Range.End)
				}
				d.state.ShowHover(start, end, text, nil)
			}
		}
	case "textDocument/definition":
		var locations []struct {
			URI                  string   `json:"uri"`
			Range                lspRange `json:"range"`
			TargetURI            string   `json:"targetUri"`
			TargetSelectionRange lspRange `json:"targetSelectionRange"`
		}
		if json.Unmarshal(e.result, &locations) != nil {
			_ = json.Unmarshal(append(append([]byte{'['}, e.result...), ']'), &locations)
		}
		if len(locations) > 0 {
			v := locations[0]
			if v.TargetURI != "" {
				v.URI = v.TargetURI
				v.Range = v.TargetSelectionRange
			}
			path := lspPath(v.URI)
			if path != "" {
				a.open(path)
				if target := a.current(); target != nil && target.path == path && target.state != nil {
					text := target.state.Buffer.String()
					target.state.Select(lspOffset(text, v.Range.Start), lspOffset(text, v.Range.End))
					target.state.ScrollTo(v.Range.Start.Line)
					a.focusEditor = true
				}
			}
		}
	}
}

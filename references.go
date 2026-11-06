package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"unicode"
	"unicode/utf8"

	"github.com/HyperparameterSI/golet/backend/ui"
)

type referenceLocation struct {
	URI   string
	Range lspRange
}
type referenceResults struct {
	name      string
	locations []referenceLocation
	snapshots []languageSnapshot
	list      ui.ListState
}

func referenceSymbol(text string, offset int) (string, int) {
	offset = max(0, min(offset, len(text)))
	start, end := offset, offset
	ident := func(r rune) bool {
		return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r)
	}
	for start > 0 {
		r, n := utf8.DecodeLastRuneInString(text[:start])
		if !ident(r) {
			break
		}
		start -= n
	}
	for end < len(text) {
		r, n := utf8.DecodeRuneInString(text[end:])
		if !ident(r) {
			break
		}
		end += n
	}
	return text[start:end], start
}

func (a *app) bindReferenceMenu(d *document) {
	kind, _ := lspLanguage(d.path)
	w := a.intelligence.servers[kind]
	if kind != "go" && kind != "typescript" {
		return
	}
	if w == nil {
		d.state.ContextMenu = nil
		return
	}
	d.state.ContextMenu = func(m *ui.Menu) {
		if m.Item("查看引用").Chosen() {
			name, offset := referenceSymbol(d.state.Buffer.String(), d.state.Cursor())
			if name == "" {
				a.notice = "请在方法名上右键查看引用"
				return
			}
			a.intelligence.references = nil
			a.requestIntelligence(w, d, referencesMethod, offset, offset)
		}
	}
}
func (a *app) receiveReferences(e intelligenceEvent) {
	if e.err != nil {
		a.notice = "查找引用失败：" + e.err.Error()
		return
	}
	var items []referenceLocation
	if err := json.Unmarshal(e.result, &items); err != nil {
		a.notice = "无法读取语言服务返回的引用位置"
		return
	}
	name, _ := referenceSymbol(e.job.target.text, e.job.offset)
	refs := &referenceResults{name: name, snapshots: e.job.docs}
	seen := make(map[referenceLocation]bool)
	for _, item := range items {
		if lspPath(item.URI) != "" && !seen[item] {
			seen[item] = true
			refs.locations = append(refs.locations, item)
		}
	}
	sort.Slice(refs.locations, func(i, j int) bool {
		x, y := refs.locations[i], refs.locations[j]
		if x.URI != y.URI {
			return x.URI < y.URI
		}
		if x.Range.Start.Line != y.Range.Start.Line {
			return x.Range.Start.Line < y.Range.Start.Line
		}
		return x.Range.Start.Character < y.Range.Start.Character
	})
	a.intelligence.references = refs
	a.terminal.showReferences = true
}
func (a *app) referencePanel(c *ui.Context, refs *referenceResults) {
	ui.Column(c.Key("references")).Fill().MinHeight(0).Background(a.theme.Sidebar).Padding(4, 10).Children(func() {
		ui.Row(c).FillWidth().Shrink(0).AlignItems(ui.Center).Children(func() {
			if a.sharedBottomPanel() {
				a.bottomPanelTabs(c)
			}
			ui.Text(c, fmt.Sprintf("%s 的引用 · %d", refs.name, len(refs.locations))).Bold().Grow(1).MinWidth(0).MaxLines(1)
			iconButton(c, "close-references", "close", "关闭引用列表", func() { a.intelligence.references = nil })
		})
		if len(refs.locations) == 0 {
			muted(c, "未找到引用")
			return
		}
		ui.List(c.Key("reference-rows"), &refs.list, len(refs.locations), func(i int) {
			loc := refs.locations[i]
			path := lspPath(loc.URI)
			label := compactPath(path)
			if a.root != "" && inside(a.root, path) {
				label, _ = filepath.Rel(a.root, path)
			}
			label = fmt.Sprintf("%s:%d:%d", label, loc.Range.Start.Line+1, loc.Range.Start.Character+1)
			action(c, label, label, func() { a.openLanguageLocation(path, loc.Range, refs.snapshots) }).FillWidth().Justify(ui.Start).Height(28).Tooltip(label)
		}).FillWidth().Grow(1).MinHeight(0)
	})
}

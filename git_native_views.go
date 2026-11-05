package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/editor"
	"github.com/HyperparameterSI/golet/backend/ui/forma"
)

type gitGraphRow struct {
	Commit GitHistory
	Lane   int
	Color  string
	Edges  []gitGraphEdge
}
type gitGraphEdge struct {
	From, To   int
	Start, End int
	Color      string
}
type gitLane struct{ Hash, Color string }

func buildGitGraph(history []GitHistory) ([]gitGraphRow, int) {
	colors := []string{"#5276d9", "#36a185", "#af78c5", "#d39a48", "#4a9dbd", "#c87083"}
	var lanes []gitLane
	var rows []gitGraphRow
	sequence, width := 0, 1
	for _, commit := range history {
		incoming := -1
		for i, l := range lanes {
			if l.Hash == commit.Hash {
				incoming = i
				break
			}
		}
		lane := incoming
		color := ""
		if incoming < 0 {
			lane = len(lanes)
			color = colors[sequence%len(colors)]
			sequence++
			lanes = append(lanes, gitLane{commit.Hash, color})
		} else {
			color = lanes[lane].Color
		}
		before := append([]gitLane(nil), lanes...)
		after := append([]gitLane(nil), before[:lane]...)
		after = append(after, before[lane+1:]...)
		parents := strings.Fields(commit.Parents)
		for i, hash := range parents {
			found := false
			for _, l := range after {
				if l.Hash == hash {
					found = true
				}
			}
			if !found {
				c := color
				if i > 0 {
					c = colors[sequence%len(colors)]
					sequence++
				}
				at := min(lane+i, len(after))
				after = append(after, gitLane{})
				copy(after[at+1:], after[at:])
				after[at] = gitLane{hash, c}
			}
		}
		target := func(hash string) int {
			for i, l := range after {
				if l.Hash == hash {
					return i
				}
			}
			return -1
		}
		r := gitGraphRow{Commit: commit, Lane: lane, Color: color}
		for i, l := range before {
			if i == lane {
				if incoming >= 0 {
					r.Edges = append(r.Edges, gitGraphEdge{i, i, 0, 24, color})
				}
				continue
			}
			r.Edges = append(r.Edges, gitGraphEdge{i, target(l.Hash), 0, 48, l.Color})
		}
		for _, hash := range parents {
			to := target(hash)
			if to >= 0 {
				r.Edges = append(r.Edges, gitGraphEdge{lane, to, 24, 48, after[to].Color})
			}
		}
		width = max(width, len(before), len(after))
		lanes = after
		rows = append(rows, r)
	}
	return rows, max(66, width*16+24)
}
func gitGraphSVG(row gitGraphRow, width int) *ui.SVG {
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="48" viewBox="0 0 %d 48">`, width, width)
	for _, e := range row.Edges {
		if e.To < 0 {
			continue
		}
		x, y := 12+16*e.From, 12+16*e.To
		mid := (e.Start + e.End) / 2
		fmt.Fprintf(&b, `<path d="M %d %d C %d %d %d %d %d %d" fill="none" stroke="%s" stroke-width="1.5"/>`, x, e.Start, x, mid, y, mid, y, e.End, e.Color)
	}
	fmt.Fprintf(&b, `<circle cx="%d" cy="24" r="3.5" fill="%s"/></svg>`, 12+16*row.Lane, row.Color)
	return ui.MustParseSVG([]byte(b.String()))
}
func (a *app) gitHistoryView(c *ui.Context) {
	p := &a.git
	history := p.state.Advanced.History
	if p.advanced.historyLoaded {
		history = p.advanced.history
	}
	if !p.advanced.historyLoaded && len(p.state.Files) > 0 {
		history = append([]GitHistory{{Hash: "working-tree", Parents: p.state.Advanced.Head, Subject: fmt.Sprintf("未提交的改动 · %d 个文件", len(p.state.Files))}}, history...)
	}
	rows, width := buildGitGraph(history)
	var key strings.Builder
	for _, h := range history {
		key.WriteString(h.Hash)
		key.WriteString(h.Parents)
	}
	if p.advanced.graphKey != key.String() {
		p.advanced.graphKey = key.String()
		p.advanced.graphCache = map[string]*ui.SVG{}
	}
	if p.advanced.graphCache == nil {
		p.advanced.graphCache = map[string]*ui.SVG{}
	}
	ui.Scroll(c.Key("git-graph")).Fill().Children(func() {
		ui.Column(c).FillWidth().Children(func() {
			for _, r := range rows {
				row := r
				h := row.Commit
				if p.advanced.query != "" && !strings.Contains(strings.ToLower(h.Hash+" "+h.Subject+" "+h.Author+" "+h.Refs), strings.ToLower(p.advanced.query)) {
					continue
				}
				ui.Button(c.Key("graph-"+h.Hash), "").FillWidth().Height(48).Padding(0, 12).Background(ui.Transparent).Border(0, ui.Transparent).Disabled(p.busy).OnClick(func() {
					p.filePath = ""
					if h.Hash == "working-tree" {
						p.tab = "变更"
						p.patch, p.diff, p.selection = nil, nil, ""
						return
					}
					a.gitServiceTask(false, "history-patch", func(g *gitService, root string) gitPanelEvent {
						patch, e := g.GitDiff(root, "", false, h.Hash)
						return gitPanelEvent{preview: true, title: h.Subject, patch: patch, err: e}
					})
				}).Children(func() {
					ui.Row(c).Fill().Gap(12).AlignItems(ui.Center).Children(func() {
						if p.advanced.query == "" {
							svg := p.advanced.graphCache[h.Hash]
							if svg == nil {
								svg = gitGraphSVG(row, width)
								p.advanced.graphCache[h.Hash] = svg
							}
							ui.Image(c, svg).Size(float32(width), 48)
						}
						ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
							ui.Text(c, h.Subject).FontSize(12).MaxLines(1)
							if h.Refs != "" {
								muted(c, h.Refs).FontSize(10).MaxLines(1)
							}
						})
						shortHash := h.Hash[:min(8, len(h.Hash))]
						if h.Hash == "working-tree" {
							shortHash = ""
						}
						muted(c, shortHash).FontSize(11)
						muted(c, h.Author).FontSize(11).Width(90).MaxLines(1)
						date := h.Date
						if len(date) > 10 {
							date = date[:10]
						}
						muted(c, date).FontSize(11).Width(80)
					})
				})
			}
			muted(c, "显示最近 100 条提交；连线可延续到更早历史。").FontSize(11).Padding(16)
		})
	})
}
func (a *app) gitPatchView(c *ui.Context) {
	p := &a.git
	raw := p.advanced.rawPatch
	lines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
	validRows := gitChangedRows(raw)
	if p.advanced.textSelect {
		if p.advanced.patchEditor == nil {
			p.advanced.patchEditor = &editor.State{Buffer: editor.NewBuffer(raw), ReadOnly: true}
		}
		p.advanced.lines = gitRowsFromSelection(raw, p.advanced.patchEditor.Selections())
	}
	if p.advanced.lines == nil {
		p.advanced.lines = map[int]bool{}
	}
	operation := "stage"
	if p.staged {
		operation = "unstage"
	}
	var file *repositoryFile
	for i := range p.state.Files {
		if p.state.Files[i].Path == p.filePath {
			file = &p.state.Files[i]
			break
		}
	}
	editable := file != nil && !file.Conflict && file.OldPath == "" && file.Index != "?" && file.Index != "A" && file.Index != "D" && file.Worktree != "D" && len(lines) <= 5000
	choose := func(rows []int, discard bool) {
		if discard {
			a.gitAsk("partial-discard", "丢弃选中修改", "", "", "")
			d := &p.dialog
			d.rows = append([]int(nil), rows...)
			d.expected = raw
			d.ref = p.filePath
		} else {
			a.gitApplyLines(rows, operation, raw, p.filePath)
		}
	}
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Padding(6, 14).Gap(8).AlignItems(ui.Center).Children(func() {
			iconButton(c, "partial-text-select", "panel", "切换拖选文本模式", func() { p.advanced.textSelect = !p.advanced.textSelect }).Size(26, 26).Padding(5)
			selected := selectedRows(p.advanced.lines)
			muted(c, fmt.Sprintf("已选 %d 行 · 点击多选，Shift 连选", len(selected))).FontSize(11).Grow(1)
			iconButton(c, "partial-apply", "check", "应用选中行", func() { choose(selected, false) }).Disabled(!editable || p.busy || len(selected) == 0)
			if !p.staged {
				iconButton(c, "partial-discard", "trash", "丢弃选中行", func() { choose(selected, true) }).Disabled(!editable || p.busy || len(selected) == 0)
			}
			iconButton(c, "partial-copy", "files", "复制选中行", func() {
				var text []string
				for _, n := range selected {
					if n < len(lines) && len(lines[n]) > 0 {
						text = append(text, lines[n][1:])
					}
				}
				copyGitText(strings.Join(text, "\n"))
			}).Disabled(len(selected) == 0)
		})
		if p.advanced.textSelect {
			p.advanced.patchEditor.Theme = a.codeTheme
			p.advanced.patchEditor.Font = ui.Font{Family: a.prefs.EditorFont, Size: float32(a.prefs.EditorSize)}
			editor.View(c.Key("patch-text-selection"), p.advanced.patchEditor).Fill()
			return
		}
		ui.Scroll(c.Key("git-patch-lines")).FillWidth().Grow(1).Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				inHunk := false
				oldLine, newLine := 0, 0
				for i, line := range lines {
					if i >= 5000 {
						muted(c, "超过 5000 行，请使用整文件操作。")
						break
					}
					idx, text := i, line
					if gitHunkHeader.MatchString(text) {
						inHunk = true
						match := gitHunkHeader.FindStringSubmatch(text)
						oldLine, _ = strconv.Atoi(match[1])
						newLine, _ = strconv.Atoi(match[3])
						var rows []int
						for j := i + 1; j < len(lines) && !gitHunkHeader.MatchString(lines[j]); j++ {
							if strings.HasPrefix(lines[j], "+") || strings.HasPrefix(lines[j], "-") {
								rows = append(rows, j)
							}
						}
						ui.Row(c.Key(i)).FillWidth().Padding(5, 12).Background(a.theme.Sidebar).AlignItems(ui.Center).Children(func() {
							muted(c, text).FontSize(11).Grow(1)
							iconButton(c, "hunk-apply", "check", "应用此区块", func() { choose(rows, false) }).Size(24, 24).Padding(5).Disabled(!editable || p.busy)
							if !p.staged {
								iconButton(c, "hunk-discard", "trash", "丢弃此区块", func() { choose(rows, true) }).Size(24, 24).Padding(5).Disabled(!editable || p.busy)
							}
						})
						continue
					}
					changed := inHunk && (strings.HasPrefix(text, "+") || strings.HasPrefix(text, "-"))
					bg := ui.Transparent
					if changed {
						if strings.HasPrefix(text, "+") {
							bg = a.codeTheme.Added.Alpha(.10)
						} else {
							bg = a.codeTheme.Deleted.Alpha(.10)
						}
					}
					oldNumber, newNumber := "", ""
					if inHunk && len(text) > 0 && strings.ContainsRune(" +-", rune(text[0])) {
						if text[0] != '+' {
							oldNumber = strconv.Itoa(oldLine)
							oldLine++
						}
						if text[0] != '-' {
							newNumber = strconv.Itoa(newLine)
							newLine++
						}
					}
					if p.advanced.lines[idx] {
						bg = a.theme.Surface
					}
					ui.Row(c.Key(idx)).FillWidth().MinHeight(22).Background(bg).Padding(0, 12).Gap(8).AlignItems(ui.Center).Children(func() {
						if changed && editable {
							on := p.advanced.lines[idx]
							forma.Checkbox(c, &on, "").Disabled(p.busy)
							if on != p.advanced.lines[idx] {
								if c.Modifiers()&ui.Shift != 0 {
									lo, hi := min(p.advanced.anchor, idx), max(p.advanced.anchor, idx)
									for n := lo; n <= hi; n++ {
										if validRows[n] {
											p.advanced.lines[n] = on
										}
									}
								} else {
									p.advanced.lines[idx] = on
								}
								p.advanced.anchor = idx
							}
						} else {
							ui.Box(c).Width(18)
						}
						muted(c, oldNumber).Width(30).FontSize(10)
						muted(c, newNumber).Width(30).FontSize(10)
						ui.Text(c, text).Font(a.prefs.EditorFont).FontSize(float32(a.prefs.EditorSize)).Wrap().MinWidth(0).Grow(1)
					})
				}
			})
		})
	})
}

func gitChangedRows(raw string) map[int]bool {
	rows := map[int]bool{}
	inHunk := false
	for i, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			inHunk = false
		}
		if gitHunkHeader.MatchString(line) {
			inHunk = true
			continue
		}
		if inHunk && (strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-")) {
			rows[i] = true
		}
	}
	return rows
}
func gitRowsFromSelection(raw string, selections []editor.Selection) map[int]bool {
	valid := gitChangedRows(raw)
	result := map[int]bool{}
	start := 0
	for i, line := range strings.Split(raw, "\n") {
		end := start + len(line)
		if valid[i] {
			for _, sel := range selections {
				lo, hi := min(sel.Anchor, sel.Head), max(sel.Anchor, sel.Head)
				if hi > lo && lo < end && hi > start {
					result[i] = true
				}
			}
		}
		start = end + 1
	}
	return result
}

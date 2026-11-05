package main

import (
	"context"
	"fmt"
	golet "github.com/HyperparameterSI/golet/backend"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/editor"
	"github.com/HyperparameterSI/golet/backend/ui/forma"
)

type gitPanel struct {
	advanced                           gitAdvancedState
	progress                           string
	filePath                           string
	visible, busy, mutating            bool
	root, err, message, tab, selection string
	state                              repositoryState
	events                             chan gitPanelEvent
	cancel                             context.CancelFunc
	next                               time.Time
	diff                               *editor.DiffState
	patch                              *editor.State
	staged                             bool
	dialog                             gitPanelDialog
}
type gitPanelDialog struct {
	ref, mode, scope, branch, expected, preview string
	rows                                        []int
	options                                     []string
	commit, log, noFF                           bool
	open                                        bool
	kind, title, value, extra, detail           string
}
type gitPanelEvent struct {
	settings                                            *GitSettings
	progressOnly                                        bool
	progress, rawPatch, info, draft, openRoot, external string
	needsCommit, committed, historyLoaded               bool
	history                                             []GitHistory
	remoteBranches                                      []string
	clearPreview                                        bool
	state                                               *repositoryState
	err                                                 error
	before, after, patch, title                         string
	preview, mutated                                    bool
	operation                                           string
}

func (a *app) toggleGit() {
	a.git.visible = !a.git.visible
	if a.git.visible {
		a.sidebar = true
		a.quick = false
		a.git.next = time.Time{}
	}
}
func (a *app) closeGit() {
	if a.gitReview != nil {
		a.gitReview.window.Destroy()
		a.gitReview = nil
	}
	if a.git.cancel != nil {
		a.git.cancel()
	}
	visible := a.git.visible
	a.git = gitPanel{visible: visible}
}
func (a *app) tickGit(c *ui.Context) {
	p := &a.git
	if p.root != a.root {
		a.closeGit()
		p = &a.git
		p.root = a.root
	}
	if p.events != nil {
		select {
		case ev := <-p.events:
			if ev.progressOnly {
				p.progress = ev.progress
				break
			}
			if ev.rawPatch != p.advanced.rawPatch && ev.preview {
				p.advanced.lines = nil
				p.advanced.patchEditor = nil
				p.advanced.rawPatch = ev.rawPatch
			}
			if ev.historyLoaded {
				p.advanced.history = ev.history
				p.advanced.historyLoaded = true
			}
			if ev.settings != nil && p.dialog.open && p.dialog.kind == "identity" {
				p.dialog.value = ev.settings.Name
				p.dialog.extra = ev.settings.Email
			}
			if ev.remoteBranches != nil {
				p.dialog.options = ev.remoteBranches
			}
			if ev.info != "" {
				p.dialog.preview = ev.info
			}
			if ev.external != "" && ev.err == nil {
				a.report(golet.Shell.OpenExternal(ev.external))
			}
			if ev.operation == "ai-message" && ev.err == nil {
				p.message = ev.draft
				if ev.info != "" {
					a.notice = ev.info
				}
			}
			if ev.committed {
				p.message = ""
			}
			if ev.needsCommit {
				a.gitAsk("large-commit", "大文件提交", ev.draft, "", "")
				p.dialog.preview = ev.info
			}

			p.busy = false
			p.mutating = false
			p.cancel = nil
			p.progress = ""
			p.next = time.Now().Add(3 * time.Second)
			if ev.err != nil {
				p.err = ev.err.Error()
			} else if ev.operation != "refresh" {
				p.err = ""
			}
			if ev.state != nil {
				p.state = *ev.state
			}
			if ev.clearPreview || ev.preview && ev.err != nil {
				p.diff = nil
				p.patch = nil
				p.selection = ""
				p.filePath = ""
			}
			if ev.preview && ev.err == nil {
				p.selection = ev.title
				if ev.patch != "" {
					p.patch = &editor.State{Buffer: editor.NewBuffer(ev.patch), ReadOnly: true}
					p.diff = nil
				} else {
					if p.diff == nil || p.diff.Old.String() != ev.before || p.diff.New.String() != ev.after {
						p.diff = &editor.DiffState{Old: editor.NewBuffer(ev.before), New: editor.NewBuffer(ev.after), ExpandUnchanged: true}
						p.diff.Old.SetLanguage(editor.LanguageFor(ev.title))
						p.diff.New.SetLanguage(editor.LanguageFor(ev.title))
					}
					p.patch = nil
				}
			}
			if ev.mutated && !ev.needsCommit {
				p.filePath = ""
				p.diff = nil
				p.patch = nil
				p.selection = ""
				a.reloadGitDocuments()
				a.refresh()
				if ev.err == nil && ev.operation == "commit" && !ev.needsCommit {
					p.message = ""
				}
			}
			if ev.openRoot != "" && ev.err == nil {
				a.setRoot(ev.openRoot)
				return
			}
		default:
		}
	}
	if p.visible && p.root != "" && !p.busy && time.Now().After(p.next) {
		a.gitRefresh()
	}
	if p.visible || p.busy {
		c.After(100 * time.Millisecond)
	}
}
func (a *app) gitTask(mutating bool, operation string, task func(context.Context, string) gitPanelEvent) {
	p := &a.git
	if p.busy || a.root == "" && operation != "clone" {
		return
	}
	if mutating {
		if a.ai.running || a.hasPendingAI() {
			p.err = "请先结束 AI 任务并处理待批准修改"
			return
		}
		for _, d := range a.docs {
			if d.editable() && d.path != "" && inside(a.root, d.path) && d.state.Buffer.Modified() {
				if !a.writeAuto(d) {
					p.err = "请先处理未保存的文件"
					return
				}
			}
		}
	}
	if p.events == nil {
		p.events = make(chan gitPanelEvent, 32)
	}
	p.busy = true
	p.mutating = mutating
	if operation != "refresh" {
		p.err = ""
	}
	root := a.root
	p.root = root
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	events := p.events
	ctx = context.WithValue(ctx, gitProgressKey{}, func(text string) {
		if len(events) < cap(events)-1 {
			select {
			case events <- gitPanelEvent{progressOnly: true, progress: text}:
			default:
			}
		}
	})
	go func() {
		defer cancel()
		ev := task(ctx, root)
		ev.mutated = mutating
		ev.operation = operation
		events <- ev
	}()
}
func (a *app) gitRefresh() {
	path, staged := a.git.filePath, a.git.staged
	a.gitTask(false, "refresh", func(ctx context.Context, root string) gitPanelEvent {
		s, e := readRepository(ctx, root)
		ev := gitPanelEvent{state: &s, err: e}
		if e == nil && path != "" {
			ev.clearPreview = true
			for _, f := range s.Files {
				if f.Path == path && (staged && f.Index != " " && f.Index != "?" || !staged && f.Worktree != " ") {
					ev.before, ev.after, ev.err = repositoryDiff(ctx, root, f, staged)
					ev.rawPatch, _ = gitServiceFor(ctx, root).GitDiff(root, f.Path, staged, "")
					if ev.err != nil && ev.rawPatch != "" {
						ev.patch = ev.rawPatch
						ev.err = nil
					}
					ev.preview = true
					ev.clearPreview = false
					ev.title = "工作区 · " + path
					if staged {
						ev.title = "暂存区 · " + path
					}
					break
				}
			}
		}
		return ev
	})
}
func (a *app) gitRun(kind, value, extra string) {
	a.gitTask(true, kind, func(ctx context.Context, root string) gitPanelEvent {
		e := repositoryAction(ctx, root, kind, value, extra)
		s, readErr := readRepository(ctx, root)
		if e == nil {
			e = readErr
		}
		return gitPanelEvent{state: &s, err: e}
	})
}
func (a *app) gitSelect(f repositoryFile, staged bool) {
	a.git.staged = staged
	a.git.filePath = f.Path
	a.gitTask(false, "", func(ctx context.Context, root string) gitPanelEvent {
		before, after, e := repositoryDiff(ctx, root, f, staged)
		raw, pe := gitServiceFor(ctx, root).GitDiff(root, f.Path, staged, "")
		patch := ""
		if e != nil && pe == nil {
			patch = raw
			e = nil
		}
		label := "工作区 · " + f.Path
		if staged {
			label = "暂存区 · " + f.Path
		}
		return gitPanelEvent{preview: true, title: label, before: before, after: after, err: e, rawPatch: raw, patch: patch}
	})
}
func (a *app) reloadGitDocuments() {
	var docs []*document
	current := a.current()
	active := -1
	for _, d := range a.docs {
		original := d
		if d.path != "" && inside(a.root, d.path) && d.editable() && !d.state.Buffer.Modified() {
			if fresh, e := loadDocument(d.path); e == nil {
				fresh.preview = d.preview
				d = fresh
			} else if os.IsNotExist(e) {
				continue
			}
		}
		if original == current {
			active = len(docs)
		}
		docs = append(docs, d)
	}
	a.docs = docs
	if active < 0 && len(docs) > 0 {
		active = min(a.active, len(docs)-1)
	}
	a.active = active
}
func (a *app) gitAsk(kind, title, value, extra, detail string) {
	a.git.dialog = gitPanelDialog{open: true, kind: kind, title: title, value: value, extra: extra, detail: detail, mode: "ff-only", commit: true}
}
func (a *app) gitSidebar(c *ui.Context) {
	p := &a.git
	ui.Box(c).Fill().Padding(0, 0, 0, 6).Background(a.theme.Surface).Children(func() {
		ui.Column(c).Fill().Radius(12, 0, 0, 12).Background(a.theme.Sidebar).Padding(12, 10).Gap(10).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "源代码管理").Bold().Grow(1)
				iconButton(c, "git-refresh", "refresh", "刷新 Git", a.gitRefresh).Disabled(p.busy)
			})
			if p.state.Root == "" {
				muted(c, "打开一个 Git 仓库开始管理").Wrap()
				if a.root != "" {
					forma.Button(c, "初始化仓库").Disabled(p.busy).OnClick(func() { a.gitAsk("init", "初始化 Git 仓库", a.root, "", "在当前项目创建 Git 仓库。") })
				}
				return
			}
			ui.Button(c.Key("git-branch"), p.state.Branch).FillWidth().Background(ui.Transparent).Border(0, ui.Transparent).Menu(func(m *ui.Menu) {
				for _, branch := range p.state.Branches {
					b := branch
					if m.Item(b).Chosen() {
						a.gitAsk("switch", "切换分支", b, "", "切换前需要提交或贮藏工作区修改。")
					}
				}
				if m.Item("新建分支…").Chosen() {
					a.gitAsk("branch", "新建分支", "", "", "从当前 HEAD 创建并切换。")
				}
			}).Disabled(p.busy)
			ui.Row(c).Gap(6).Children(func() {
				for _, tab := range []string{"变更", "历史", "仓库"} {
					label := tab
					bg := ui.Transparent
					if p.tab == label || p.tab == "" && label == "变更" {
						bg = a.theme.Background
					}
					action(c, "git-tab-"+label, label, func() {
						p.tab = label
						if label == "历史" {
							p.selection = ""
							p.filePath = ""
							p.patch = nil
							p.diff = nil
							p.advanced.historyLoaded = false
							p.advanced.ref = ""
						}
					}).Background(bg)
				}
			})
			if p.tab == "仓库" {
				a.gitRepositoryNavigation(c)
				return
			}
			if p.tab == "历史" {
				forma.Input(c, &p.advanced.query, forma.InputOptions{Placeholder: "搜索提交、作者、哈希…"}).FillWidth()
				muted(c, p.advanced.ref).FontSize(11).Wrap()
				action(c, "git-all-history", "全部分支", func() {
					p.advanced.historyLoaded = false
					p.advanced.ref = ""
					p.selection = ""
					p.patch = nil
					p.diff = nil
				}).FillWidth()
				ui.Box(c).Grow(1)
				return
			}
			forma.Input(c, &p.advanced.fileQuery, forma.InputOptions{Placeholder: "搜索修改文件…"}).FillWidth()

			ui.Scroll(c.Key("git-files")).Grow(1).FillWidth().Children(func() {
				ui.Column(c).FillWidth().Gap(12).Children(func() { a.gitFileGroup(c, true); a.gitFileGroup(c, false) })
			})
			ui.Column(c).FillWidth().Background(a.theme.Background).Radius(9).Padding(10).Children(func() {
				forma.Checkbox(c, &p.advanced.pushAfter, "提交后推送")
				ui.TextAreaBase(c.Key("git-commit-message"), &p.message).FillWidth().Height(60).Placeholder("提交说明…").FontSize(12).Disabled(p.busy)
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					count := 0
					for _, f := range p.state.Files {
						if f.Index != " " && f.Index != "?" {
							count++
						}
					}
					iconButton(c, "git-ai-message", "sparkles", "AI 生成提交说明", a.gitGenerateMessage).Size(26, 26).Padding(5).Disabled(p.busy || count == 0)
					muted(c, fmt.Sprintf("%d 项已暂存", count)).Grow(1).FontSize(11)
					iconButton(c, "git-commit", "check", "提交已暂存的修改", func() { a.gitCommit(false, p.message) }).Disabled(p.busy || count == 0 || strings.TrimSpace(p.message) == "")
				})
			})
		})
	})
}
func (a *app) gitFileGroup(c *ui.Context, staged bool) {
	p := &a.git
	var files []repositoryFile
	for _, f := range p.state.Files {
		if staged && f.Index != " " && f.Index != "?" || !staged && f.Worktree != " " {
			files = append(files, f)
		}
	}
	title, operation, symbol := "更改", "stage", "plus"
	if staged {
		title, operation, symbol = "已暂存", "unstage", "minus"
	}
	ui.Column(c.Key(title)).FillWidth().Gap(3).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			muted(c, fmt.Sprintf("%s  %d", title, len(files))).FontSize(11).Grow(1)
			iconButton(c, "all-"+operation, symbol, map[bool]string{true: "全部取消暂存", false: "全部暂存"}[staged], func() { a.gitRun(operation, "", "") }).Size(24, 24).Padding(5).Disabled(p.busy || len(files) == 0)
		})
		for _, file := range files {
			if p.advanced.fileQuery != "" && !strings.Contains(strings.ToLower(file.Path), strings.ToLower(p.advanced.fileQuery)) {
				continue
			}
			f := file
			status := f.Worktree
			if staged {
				status = f.Index
			}
			if f.Conflict {
				status = "!"
			}
			ui.Row(c.Key(f.Path)).FillWidth().Gap(3).AlignItems(ui.Center).Children(func() {
				ui.Button(c.Key("select"), "").Grow(1).MinWidth(0).Padding(5).Background(ui.Transparent).Border(0, ui.Transparent).Disabled(p.busy).Tooltip(f.Path).OnClick(func() { a.gitSelect(f, staged) }).ContextMenu(func(m *ui.Menu) {
					a.gitFileExtras(m, f)
					if m.Item("打开文件").Chosen() {
						p.visible = false
						a.open(filepath.Join(a.root, f.Path))
					}
					if !staged && !f.Conflict && f.Index != "?" && m.Item("丢弃未暂存修改…").Chosen() {
						a.gitAsk("discard", "丢弃修改", f.Path, "", "仅恢复此文件至暂存区版本，此操作不可撤销。")
					}
				}).Children(func() {
					ui.Row(c).FillWidth().Gap(6).Children(func() {
						icon(c, "file", 13)
						ui.Text(c, filepath.Base(f.Path)).Grow(1).MinWidth(0).MaxLines(1).FontSize(12)
						muted(c, status).FontSize(11)
					})
				})
				iconButton(c, "change-"+operation, symbol, map[bool]string{true: "取消暂存", false: "暂存"}[staged], func() { a.gitRun(operation, f.Path, "") }).Size(22, 24).Padding(4).Disabled(p.busy)
			})
		}
	})
}
func (a *app) gitContent(c *ui.Context) {
	p := &a.git
	ui.Column(c).Fill().MinWidth(0).Children(func() {
		ui.Row(c).FillWidth().Height(42).Padding(0, 12).Gap(3).AlignItems(ui.Center).Children(func() {
			icon(c, "git", 17)
			name := filepath.Base(a.root)
			if a.root == "" {
				name = "Git"
			}
			ui.Text(c, name).FontSize(12)
			if p.state.Upstream != "" {
				muted(c, fmt.Sprintf("↑ %d  ↓ %d", p.state.Ahead, p.state.Behind)).FontSize(11)
			}
			ui.Box(c).Grow(1)
			for _, item := range []struct{ kind, icon, title string }{{"fetch", "refresh", "抓取"}, {"pull", "arrow-down", "拉取"}, {"push", "arrow-up", "推送"}, {"merge", "merge", "合并"}, {"stash", "archive", "贮藏"}} {
				op := item
				iconButton(c, "git-"+op.kind, op.icon, op.title, func() {
					value, extra, detail := "", "", ""
					switch op.kind {
					case "fetch", "pull", "push":
						if len(p.state.Remotes) > 0 {
							value = p.state.Remotes[0]
						}
						extra = p.state.Branch
						for _, remote := range p.state.Remotes {
							if strings.HasPrefix(p.state.Upstream, remote+"/") {
								value = remote
								extra = strings.TrimPrefix(p.state.Upstream, remote+"/")
								break
							}
						}
						detail = "使用已有 Git 凭据；推送不强制覆盖。"
					case "merge":
						detail = "将选中的本地分支合并到当前分支。"
					case "stash":
						detail = "贮藏已跟踪和未跟踪文件，保留已有贮藏。"
					}
					a.gitAsk(op.kind, op.title, value, extra, detail)
				}).Disabled(p.busy || p.state.Root == "")
			}
			iconButton(c, "git-more", "settings", "仓库操作", nil).Disabled(p.busy).Menu(func(m *ui.Menu) {
				a.gitAdvancedMenu(m)
				if m.Item("配置远端…").Chosen() {
					remote := "origin"
					if len(p.state.Remotes) > 0 {
						remote = p.state.Remotes[0]
					}
					a.gitAsk("remote", "配置远端", remote, "", "填写完整地址，仅修改当前仓库配置。")
				}
				if m.Item("提交作者…").Chosen() {
					a.gitAsk("identity", "提交作者", "", "", "仅设置当前仓库的作者姓名和邮箱。")
					a.gitServiceTask(false, "identity-read", func(g *gitService, root string) gitPanelEvent {
						settings, e := g.GitSettings(root)
						return gitPanelEvent{settings: &settings, err: e}
					})
				}
				for _, stash := range p.state.Stashes {
					s := stash
					if m.Item("恢复贮藏 · " + s.Label).Chosen() {
						a.gitAsk("stash-apply", "恢复贮藏", s.Hash, "", "恢复后仍保留贮藏；工作区必须没有修改。")
					}
				}
			})
			iconButton(c, "git-close", "close", "返回编辑器", func() { p.visible = false })
		})
		if p.busy {
			ui.Row(c).FillWidth().Padding(6, 14).AlignItems(ui.Center).Children(func() {
				text := "正在处理…"
				if p.progress != "" {
					text = p.progress
				}
				muted(c, text).MaxLines(3).FontSize(11).Grow(1)
				iconButton(c, "git-cancel", "stop", "停止 Git 操作", func() {
					if p.cancel != nil {
						p.cancel()
					}
				}).Size(22, 22).Padding(4)
			})
		}
		if p.err != "" {
			ui.Text(c, p.err).TextColor(a.theme.Danger).FontSize(12).Wrap().Padding(12)
		}
		if p.state.Operation != "" {
			ui.Row(c).FillWidth().Padding(8, 14).Gap(8).AlignItems(ui.Center).Children(func() {
				muted(c, "解决冲突并暂存后继续 "+p.state.Operation).Grow(1).Wrap()
				for _, kind := range []string{"continue", "abort"} {
					mode := kind
					label := "继续"
					if mode == "abort" {
						label = "中止"
					}
					forma.Button(c, label).Disabled(p.busy).OnClick(func() { a.gitAsk("resolve", label, "", "", ""); p.dialog.mode = mode })
				}
			})
		}
		if p.selection != "" {
			ui.Row(c).FillWidth().Padding(4, 16).AlignItems(ui.Center).Children(func() {
				muted(c, p.selection).FontSize(11).Grow(1).MaxLines(1)
				if p.filePath != "" && p.advanced.rawPatch != "" {
					iconButton(c, "git-line-mode", "code", "切换逐行操作", func() { p.advanced.lineMode = !p.advanced.lineMode }).Size(26, 26).Padding(5)
				}
				if p.tab == "历史" {
					iconButton(c, "history-back", "history", "返回提交图表", func() { p.selection = ""; p.patch = nil; p.diff = nil }).Size(26, 26).Padding(5)
				}
				if p.filePath != "" {
					iconButton(c, "git-open-file", "external", "在编辑器中打开", func() { a.open(filepath.Join(a.root, p.filePath)) }).Size(26, 26).Padding(5)
				}
			})
		}
		if p.tab == "历史" && p.selection == "" {
			a.gitHistoryView(c)
		} else if p.advanced.lineMode && p.filePath != "" && p.advanced.rawPatch != "" {
			a.gitPatchView(c)
		} else if p.diff != nil {
			p.diff.Theme = a.codeTheme
			p.diff.Font = ui.Font{Family: a.prefs.EditorFont, Size: float32(a.prefs.EditorSize)}
			editor.DiffView(c.Key("git-diff"), p.diff).Fill()
		} else if p.patch != nil {
			p.patch.Theme = a.codeTheme
			p.patch.Font = ui.Font{Family: a.prefs.EditorFont, Size: float32(a.prefs.EditorSize)}
			editor.View(c.Key("git-patch"), p.patch).Fill()
		} else {
			ui.Column(c).Grow(1).FillWidth().AlignItems(ui.Center).Justify(ui.Center).Gap(12).Children(func() {
				icon(c, "git", 32)
				text := "选择一个文件，查看修改"
				if p.state.Root == "" {
					text = "仅管理当前打开的 Git 仓库"
				} else if len(p.state.Files) == 0 {
					text = "工作区干净"
				}
				muted(c, text)
			})
		}
	})
}
func (a *app) gitDialog(c *ui.Context) {
	p := &a.git
	d := &p.dialog
	if !d.open {
		return
	}
	forma.Dialog(c.Key("git-dialog"), &d.open, d.title, func() {
		if d.detail != "" {
			muted(c, d.detail).Wrap()
		}
		if !a.gitAdvancedDialog(c, d) {
			switch d.kind {
			case "fetch", "pull", "push":
				settingsField(c, "远端", func() { forma.Select(c, &d.value, p.state.Remotes).FillWidth() })
				if d.kind != "fetch" {
					settingsField(c, "目标分支", func() { forma.Input(c, &d.extra).FillWidth() })
				}
			case "init", "switch", "discard", "stash-apply", "trash":
				ui.Text(c, d.value).Wrap()
			case "merge":
				settingsField(c, "本地分支", func() { forma.Select(c, &d.value, p.state.Branches).FillWidth() })
			case "remote", "identity":
				first, second := "远端名称", "仓库地址"
				if d.kind == "identity" {
					first, second = "姓名", "邮箱"
				}
				settingsField(c, first, func() { forma.Input(c.Key("value"), &d.value).FillWidth() })
				settingsField(c, second, func() { forma.Input(c.Key("extra"), &d.extra).FillWidth() })
			default:
				forma.Input(c, &d.value, forma.InputOptions{Placeholder: "名称或说明"}).FillWidth().AutoFocus()
			}
		}
		if p.err != "" {
			ui.Text(c, p.err).TextColor(a.theme.Danger).Wrap()
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			action(c, "git-dialog-cancel", "取消", func() { d.open = false })
			forma.Button(c, "确定").Disabled(p.busy).OnClick(func() {
				copy := *d
				d.open = false
				if !a.gitSubmitAdvanced(copy) {
					a.gitRun(copy.kind, copy.value, copy.extra)
				}
			})
		})
	})
}

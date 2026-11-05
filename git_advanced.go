package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	golet "github.com/HyperparameterSI/golet/backend"
	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/editor"
	"github.com/HyperparameterSI/golet/backend/ui/forma"
)

type gitProgressKey struct{}

func gitServiceFor(ctx context.Context, root string) *gitService {
	emit, _ := ctx.Value(gitProgressKey{}).(func(string))
	return &gitService{ctx: ctx, root: root, progress: emit}
}

type gitAdvancedState struct {
	collapsed                       map[string]bool
	textSelect                      bool
	patchEditor                     *editor.State
	graphCache                      map[string]*ui.SVG
	graphKey                        string
	rawPatch, fileQuery, query, ref string
	lines                           map[int]bool
	lineMode, pushAfter             bool
	history                         []GitHistory
	historyLoaded                   bool
	anchor                          int
}

func (a *app) gitServiceTask(mutate bool, kind string, fn func(*gitService, string) gitPanelEvent) {
	a.gitTask(mutate, kind, func(ctx context.Context, root string) gitPanelEvent {
		ev := fn(gitServiceFor(ctx, root), root)
		if mutate {
			state, e := readRepository(ctx, root)
			ev.state = &state
			if ev.err == nil {
				ev.err = e
			}
		}
		return ev
	})
}
func (a *app) gitBranchMenu(m *ui.Menu, b GitBranch) {
	ref := "refs/heads/" + b.Name
	if b.Remote {
		ref = "refs/remotes/" + b.Name
	}
	for _, op := range []struct{ kind, label string }{{"checkout", "检出分支…"}, {"merge", "合并到当前分支…"}, {"rebase", "当前分支变基到此处…"}, {"compare", "与当前分支比较"}, {"rename", "重命名…"}, {"delete", "删除本地分支…"}, {"track", "设置跟踪分支…"}, {"push", "推送此分支…"}} {
		if b.Remote && (op.kind == "rename" || op.kind == "delete" || op.kind == "track" || op.kind == "push") {
			continue
		}
		if m.Item(op.label).Chosen() {
			a.gitAsk("branch-action", op.label, b.Name, "", ref)
			d := &a.git.dialog
			d.ref = ref
			d.mode = op.kind
			d.branch = a.git.state.Branch
			if d.branch == "游离 HEAD" {
				d.branch = ""
			}
			if b.Remote {
				_, d.value, _ = strings.Cut(b.Name, "/")
			}
			if op.kind == "track" {
				d.value = ""
			}
			if op.kind == "push" && len(a.git.state.Remotes) > 0 {
				d.extra = a.git.state.Remotes[0]
			}
		}
	}
	if m.Item("复制分支名").Chosen() {
		golet.Clipboard.WriteText(b.Name)
	}
}
func (a *app) gitReference(ref string) {
	a.git.advanced.ref = ref
	a.git.filePath = ""
	a.git.selection = ""
	a.git.patch = nil
	a.git.diff = nil
	a.git.tab = "历史"
	a.gitServiceTask(false, "history", func(g *gitService, root string) gitPanelEvent {
		h, e := g.GitRefHistory(root, ref)
		return gitPanelEvent{history: h, historyLoaded: true, err: e}
	})
}

// Keep repository navigation compact and consistent with the file explorer.
func (a *app) gitReferenceGroup(c *ui.Context, title string, count int, build func()) {
	if count == 0 {
		return
	}
	p := &a.git.advanced
	if p.collapsed == nil {
		p.collapsed = map[string]bool{}
	}
	ui.Column(c.Key("group-" + title)).FillWidth().Gap(2).Children(func() {
		ui.Button(c.Key("toggle-"+title), "").Label(title).FillWidth().Height(28).Padding(4, 6).Border(0, ui.Transparent).Background(ui.Transparent).Radius(6).OnClick(func() { p.collapsed[title] = !p.collapsed[title] }).Children(func() {
			ui.Row(c).Fill().Gap(6).AlignItems(ui.Center).Children(func() {
				arrow := "chevron-down"
				if p.collapsed[title] {
					arrow = "chevron-right"
				}
				icon(c, arrow, 12)
				muted(c, title).FontSize(11).Grow(1)
				muted(c, fmt.Sprint(count)).FontSize(10)
			})
		})
		if !p.collapsed[title] {
			build()
		}
	})
}
func (a *app) gitReferenceRow(c *ui.Context, key, title, symbol string, current bool, click func()) ui.Element {
	return ui.Button(c.Key(key), "").Label(title).FillWidth().Height(32).Padding(5, 8, 5, 24).Radius(6).Border(0, ui.Transparent).Background(ui.Transparent).Disabled(a.git.busy).OnClick(click).Children(func() {
		ui.Row(c).Fill().Gap(8).AlignItems(ui.Center).Children(func() {
			icon(c, symbol, 14)
			ui.Text(c, title).FontSize(12).MaxLines(1).MinWidth(0).Grow(1)
			if current {
				icon(c, "check", 12)
			}
		})
	})
}
func (a *app) gitRepositoryNavigation(c *ui.Context) {
	p := &a.git
	s := p.state.Advanced
	ui.Scroll(c.Key("git-references")).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			for _, group := range []struct {
				title  string
				remote bool
			}{{"分支", false}} {
				count := 0
				for _, b := range s.Branches {
					if b.Remote == group.remote {
						count++
					}
				}
				a.gitReferenceGroup(c, group.title, count, func() {
					for _, branch := range s.Branches {
						b := branch
						if b.Remote != group.remote {
							continue
						}
						ref := "refs/heads/" + b.Name
						if b.Remote {
							ref = "refs/remotes/" + b.Name
						}
						a.gitReferenceRow(c, ref, b.Name, "git", !b.Remote && b.Name == s.Branch, func() { a.gitReference(ref) }).ContextMenu(func(m *ui.Menu) { a.gitBranchMenu(m, b) })
					}
				})
			}
			a.gitReferenceGroup(c, "标签", len(s.Tags), func() {
				for _, tag := range s.Tags {
					t := tag
					a.gitReferenceRow(c, "git-tag-"+t.Name, t.Name, "tag", false, func() { a.gitReference("refs/tags/" + t.Name) })
				}
			})
			a.gitReferenceGroup(c, "远端", len(s.Remotes), func() {
				for _, remote := range s.Remotes {
					r := remote
					a.gitReferenceRow(c, "remote-"+r.Name, r.Name, "remote", false, func() { a.gitAsk("remote-info", "远端 · "+r.Name, r.Name, r.URL, "") }).Tooltip(r.URL).ContextMenu(func(m *ui.Menu) {
						if m.Item("在浏览器打开").Chosen() {
							a.gitServiceTask(false, "remote-browser", func(g *gitService, root string) gitPanelEvent {
								raw, e := g.runGit(root, "remote", "get-url", "--", r.Name)
								if e != nil {
									return gitPanelEvent{err: e}
								}
								address, e := g.gitWebURL(raw)
								return gitPanelEvent{external: address, err: e}
							})
						}
					})
					for _, branch := range s.Branches {
						b := branch
						if !b.Remote || !strings.HasPrefix(b.Name, r.Name+"/") {
							continue
						}
						ref := "refs/remotes/" + b.Name
						a.gitReferenceRow(c, ref, strings.TrimPrefix(b.Name, r.Name+"/"), "git", false, func() { a.gitReference(ref) }).Padding(5, 8, 5, 42).ContextMenu(func(m *ui.Menu) { a.gitBranchMenu(m, b) })
					}

				}
			})
			a.gitReferenceGroup(c, "贮藏", len(s.Stashes), func() {
				for _, stash := range s.Stashes {
					v := stash
					a.gitReferenceRow(c, v.Hash, v.Subject, "archive", false, func() {
						p.filePath = ""
						a.gitServiceTask(false, "stash-preview", func(g *gitService, root string) gitPanelEvent {
							patch, e := g.GitStashDiff(root, v.Hash)
							return gitPanelEvent{preview: true, title: v.Subject, patch: patch, err: e}
						})
					}).ContextMenu(func(m *ui.Menu) {
						for _, op := range []struct{ kind, label string }{{"apply", "恢复（保留贮藏）…"}, {"pop", "恢复并删除…"}, {"drop", "删除贮藏…"}} {
							if m.Item(op.label).Chosen() {
								a.gitAsk("stash-action", op.label, v.Hash, "", v.Subject)
								a.git.dialog.mode = op.kind
							}
						}
						if m.Item("复制提交哈希").Chosen() {
							golet.Clipboard.WriteText(v.Hash)
						}
					})
				}
			})
		})
	})
}
func (a *app) gitAdvancedMenu(m *ui.Menu) {
	if m.Item("克隆仓库…").Chosen() {
		a.gitAsk("clone", "克隆并打开仓库", "", a.root, "克隆到新目录，完成后切换当前项目。")
		a.git.dialog.ref = ""
	}
	if m.Item("AI 检查代码变更…").Chosen() {
		a.showGitReview(false)
	}
	if m.Item("Open CodeReview 检查…").Chosen() {
		a.showGitReview(true)
	}
	if m.Item("在 Finder 中显示").Chosen() {
		golet.Shell.ShowItemInFolder(a.root)
	}
	if m.Item("打开内置终端").Chosen() {
		if !a.terminal.visible {
			a.toggleTerminal()
		}
	}
}
func (a *app) gitAdvancedDialog(c *ui.Context, d *gitPanelDialog) bool {
	p := &a.git
	switch d.kind {
	case "branch-action":
		ui.Text(c, d.ref).FontSize(12).Wrap()
		muted(c, "当前分支："+d.branch).Wrap()
		switch d.mode {
		case "rename", "checkout", "push":
			if d.mode == "push" {
				settingsField(c, "远端", func() { forma.Select(c, &d.extra, p.state.Remotes).FillWidth() })
			}
			if d.mode != "checkout" || strings.HasPrefix(d.ref, "refs/remotes/") {
				settingsField(c, "分支名称", func() { forma.Input(c, &d.value).FillWidth() })
			}
		case "track":
			var refs []string
			for _, b := range p.state.Advanced.Branches {
				if b.Remote {
					refs = append(refs, "refs/remotes/"+b.Name)
				}
			}
			forma.Select(c, &d.value, refs).FillWidth()
		case "rebase":
			muted(c, "将重写当前分支的提交历史，适用于尚未推送的提交。").Wrap()
		case "delete":
			muted(c, "仅删除已合并的本地分支，不强制删除。").Wrap()
		}
	case "pull":
		settingsField(c, "远端", func() { forma.Select(c, &d.value, p.state.Remotes).FillWidth() })
		settingsField(c, "目标分支", func() { forma.Input(c, &d.extra).FillWidth() })
		if len(d.options) > 0 {
			forma.Select(c, &d.extra, d.options).FillWidth()
		}
		forma.Button(c, "读取远端分支").Disabled(p.busy).OnClick(func() {
			remote := d.value
			a.gitServiceTask(false, "remote-branches", func(g *gitService, root string) gitPanelEvent {
				list, e := g.GitRemoteBranches(root, remote)
				return gitPanelEvent{remoteBranches: list, err: e}
			})
		})
		settingsField(c, "拉取方式", func() {
			gitChoice(c, &d.mode, []gitOption{{"ff-only", "仅快进"}, {"merge", "合并"}, {"rebase", "变基"}}).FillWidth()
		})
		if d.mode == "merge" {
			forma.Checkbox(c, &d.commit, "立即提交合并")
			forma.Checkbox(c, &d.log, "包含合并提交信息")
			forma.Checkbox(c, &d.noFF, "始终创建合并提交")
		}
	case "ignore":
		ui.Text(c, d.ref).Wrap()
		settingsField(c, "忽略方式", func() {
			gitChoice(c, &d.mode, []gitOption{{"exact", "此文件"}, {"extension", "相同扩展名"}, {"directory", "上级目录"}, {"custom", "自定义规则"}}).FillWidth()
		})
		if d.mode == "directory" || d.mode == "custom" {
			forma.Input(c, &d.value, forma.InputOptions{Placeholder: "上级目录或单行规则"}).FillWidth()
		}
		settingsField(c, "作用范围", func() {
			gitChoice(c, &d.scope, []gitOption{{"project", "当前项目 · .gitignore"}, {"local", "当前仓库 · 仅本机"}, {"global", "所有仓库 · 全局"}}).FillWidth()
		})
		if d.scope == "global" {
			ui.Text(c, "全局规则影响当前用户的所有仓库。").TextColor(a.theme.Danger).Wrap()
		}
		forma.Button(c, "预览规则").Disabled(p.busy).OnClick(func() {
			copy := *d
			a.gitServiceTask(false, "ignore-preview", func(g *gitService, root string) gitPanelEvent {
				rule, e := g.GitIgnorePreview(root, copy.ref, copy.mode, copy.value, copy.scope)
				return gitPanelEvent{info: rule.Pattern + "\n" + rule.Location + "\n" + rule.Description, err: e}
			})
		})
		if d.preview != "" {
			muted(c, d.preview).Wrap()
		}
	case "clone":
		settingsField(c, "仓库地址", func() { forma.Input(c, &d.value).FillWidth() })
		settingsField(c, "保存位置", func() { forma.Input(c, &d.extra).FillWidth() })
		forma.Button(c, "选择目录").OnClick(func() {
			paths, e := golet.Dialog.Open(golet.OpenDialogOptions{Parent: a.win, Title: "选择保存目录", Directory: true})
			if e != nil {
				p.err = e.Error()
			} else if len(paths) > 0 {
				d.extra = paths[0]
			}
		})
		settingsField(c, "新目录名称", func() { forma.Input(c, &d.ref).FillWidth() })
	case "remote-info":
		ui.Text(c, d.value).Bold()
		muted(c, d.extra).Wrap()
		forma.Button(c, "编辑远端地址").OnClick(func() {
			a.gitAsk("remote", "配置远端", d.value, "", "请填写完整地址；已有独立推送地址会保留。")
		})
	case "large-commit":
		ui.Text(c, d.preview).Wrap()
		muted(c, "以上暂存文件大于 10 MB，确认继续提交？").Wrap()
	case "partial-discard":
		muted(c, fmt.Sprintf("丢弃 %d 行修改，保留其他修改及暂存内容。此操作无法撤销。", len(d.rows))).Wrap()
	case "resolve":
		ui.Text(c, map[string]string{"continue": "继续当前合并／变基", "abort": "中止当前合并／变基并恢复之前的状态；当前冲突解决内容会丢失"}[d.mode]).Wrap()
	case "stash-action":
		ui.Text(c, d.detail).Wrap()
		if d.mode == "drop" {
			muted(c, "删除后无法直接从贮藏列表恢复，请确认。").Wrap()
		}
	default:
		return false
	}
	return true
}
func (a *app) gitSubmitAdvanced(d gitPanelDialog) bool {
	switch d.kind {
	case "branch-action":
		a.gitServiceTask(d.mode != "compare", "branch-action", func(g *gitService, root string) gitPanelEvent {
			value := d.value
			if d.mode == "push" {
				value = d.extra + ":" + value
			}
			out, e := g.GitBranchAction(root, d.ref, d.mode, value, d.branch)
			return gitPanelEvent{preview: d.mode == "compare", patch: out, title: d.title, err: e}
		})
	case "pull":
		a.gitServiceTask(true, "pull", func(g *gitService, root string) gitPanelEvent {
			return gitPanelEvent{err: g.GitPull(root, d.value, d.extra, GitPullOptions{Mode: d.mode, Commit: d.commit, IncludeLog: d.log, NoFF: d.noFF})}
		})
	case "ignore":
		a.gitServiceTask(true, "ignore", func(g *gitService, root string) gitPanelEvent {
			return gitPanelEvent{err: g.GitIgnoreSave(root, d.ref, d.mode, d.value, d.scope)}
		})
	case "clone":
		a.gitTask(true, "clone", func(ctx context.Context, root string) gitPanelEvent {
			project, e := gitServiceFor(ctx, root).GitCloneProject(d.value, d.extra, d.ref)
			return gitPanelEvent{openRoot: project.Path, err: e}
		})
	case "trash":
		a.gitServiceTask(true, "trash", func(g *gitService, root string) gitPanelEvent {
			return gitPanelEvent{err: g.GitFileAction(root, d.value, "remove")}
		})
	case "remote-info":
		return true
	case "large-commit":
		a.gitCommit(true, d.value)
	case "partial-discard":
		a.gitApplyLines(d.rows, "discard", d.expected, d.ref)
	case "resolve":
		a.gitServiceTask(true, "resolve", func(g *gitService, root string) gitPanelEvent {
			return gitPanelEvent{err: g.GitResolveOperation(root, d.mode)}
		})
	case "stash-action":
		a.gitServiceTask(true, "stash-action", func(g *gitService, root string) gitPanelEvent {
			var e error
			if d.mode == "drop" {
				e = g.GitStashDrop(root, d.value)
			} else {
				e = g.GitStashApply(root, d.value, d.mode == "pop")
			}
			return gitPanelEvent{err: e}
		})
	default:
		return false
	}
	return true
}
func (a *app) gitCommit(confirmed bool, message string) {
	push := a.git.advanced.pushAfter
	a.gitServiceTask(true, "commit", func(g *gitService, root string) gitPanelEvent {
		if !confirmed {
			files, e := g.GitLargeFiles(root)
			if e != nil {
				return gitPanelEvent{err: e}
			}
			if len(files) > 0 {
				var lines []string
				for _, f := range files {
					lines = append(lines, fmt.Sprintf("%s · %.1f MB", f.Path, float64(f.Size)/(1024*1024)))
				}
				return gitPanelEvent{needsCommit: true, info: strings.Join(lines, "\n"), draft: message}
			}
		}
		_, e := g.GitCommit(root, message)
		if e != nil {
			return gitPanelEvent{err: e}
		}
		ev := gitPanelEvent{committed: true}
		if push {
			s, err := g.GitSnapshot(root)
			if err != nil {
				ev.err = err
				return ev
			}
			remote, branch := "", s.Branch
			for _, r := range s.Remotes {
				if strings.HasPrefix(s.Upstream, r.Name+"/") {
					remote = r.Name
					branch = strings.TrimPrefix(s.Upstream, r.Name+"/")
					break
				}
				if remote == "" || r.Name == "origin" {
					remote = r.Name
				}
			}
			if remote == "" {
				ev.err = fmt.Errorf("提交成功，但尚未配置远端，请配置后推送")
			} else if e = g.GitNetwork(root, "push", remote, branch); e != nil {
				ev.err = fmt.Errorf("提交成功，但推送未完成：%w", e)
			}
		}
		return ev
	})
}
func (a *app) gitFileExtras(m *ui.Menu, f repositoryFile) {
	if m.Item("在 Finder 中显示").Chosen() {
		golet.Shell.ShowItemInFolder(filepath.Join(a.root, f.Path))
	}
	if m.Item("复制相对路径").Chosen() {
		golet.Clipboard.WriteText(f.Path)
	}
	if m.Item("移到废纸篓…").Chosen() {
		a.gitAsk("trash", "移到废纸篓", f.Path, "", "将此文件移到废纸篓。")
	}
	if f.Index == "?" && m.Item("忽略…").Chosen() {
		a.gitAsk("ignore", "忽略文件", "", "", "")
		d := &a.git.dialog
		d.ref = f.Path
		d.mode = "exact"
		d.scope = "project"
	}
}
func (a *app) gitApplyLines(rows []int, op, expected, path string) {
	copy := append([]int(nil), rows...)
	a.gitServiceTask(true, "partial", func(g *gitService, root string) gitPanelEvent {
		return gitPanelEvent{err: g.GitApplyLines(root, path, expected, copy, op)}
	})
}
func selectedRows(m map[int]bool) []int {
	var out []int
	for n, yes := range m {
		if yes {
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}

func copyGitText(text string) { golet.Clipboard.WriteText(text) }

type gitOption struct{ Value, Label string }

func gitChoice(c *ui.Context, value *string, options []gitOption) ui.Element {
	var labels []string
	label := ""
	for _, o := range options {
		labels = append(labels, o.Label)
		if o.Value == *value {
			label = o.Label
		}
	}
	e := forma.Select(c, &label, labels)
	for _, o := range options {
		if o.Label == label {
			*value = o.Value
			break
		}
	}
	return e
}

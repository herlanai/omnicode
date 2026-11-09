package main

import (
	"os"
	"os/exec"
	"path/filepath"

	golet "github.com/HyperparameterSI/golet/backend"
	"github.com/HyperparameterSI/golet/backend/ui"
)

type workspaceFolder struct {
	path      string
	entries   []*entry
	collapsed bool
}

func readWorkspaceFolder(path string) (*workspaceFolder, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	entries, err := children(abs)
	if err != nil {
		return nil, err
	}
	return &workspaceFolder{path: abs, entries: entries}, nil
}
func (a *app) folderForPath(path string) *workspaceFolder {
	var owner *workspaceFolder
	for _, f := range a.folders {
		if inside(f.path, path) && (owner == nil || len(f.path) > len(owner.path)) {
			owner = f
		}
	}
	return owner
}
func (a *app) activateForPath(path string) bool {
	f := a.folderForPath(path)
	return f == nil || a.activateFolder(f)
}
func (a *app) chooseFolder(path string) {
	f, err := readWorkspaceFolder(path)
	if err != nil {
		a.report(err)
		return
	}
	if a.root == "" {
		a.replaceFolder(f)
		if a.root == f.path {
			a.leavePage()
		}
		return
	}
	for _, existing := range a.folders {
		if existing.path == f.path {
			if a.activateFolder(existing) {
				existing.collapsed = false
				a.leavePage()
			}
			return
		}
	}
	answer, err := golet.Dialog.Message(golet.MessageOptions{Parent: a.win, Message: "如何打开「" + filepath.Base(f.path) + "」？", Detail: "当前项目：" + compactPath(a.root) + "\n新项目：" + compactPath(f.path), Buttons: []string{"添加到当前窗口", "替换当前项目", "在新窗口打开", "取消"}, DefaultButton: 0, CancelButton: 3})
	if err != nil {
		a.report(err)
		return
	}
	switch answer.Button {
	case 0:
		a.addFolder(f)
	case 1:
		a.replaceFolder(f)
		if a.root == f.path {
			a.leavePage()
			a.quick = false
			a.git.visible = false
		}
	case 2:
		a.report(launchProjectWindow(f.path))
	}
}
func launchProjectWindow(path string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "--", path)
	if err = cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
func (a *app) addFolder(f *workspaceFolder) {
	a.folders = append(a.folders, f)
	a.sidebar = true
	a.git.visible = false
	a.quick = false
	a.fileFilter.close()
	a.ai.visible = false
	a.leavePage()
	if a.projects != nil {
		a.report(a.projects.remember(f.path))
		a.loadRecent()
	}
}
func (a *app) replaceFolder(f *workspaceFolder) {
	old := a.root
	if f.path == old {
		return
	}
	// Preflight every affected tab before removing any, so cancellation is atomic.
	var closing []int
	for i, d := range a.docs {
		if owner := a.folderForPath(d.path); owner != nil && owner.path == old {
			if !a.canClose(d) {
				return
			}
			closing = append(closing, i)
		}
	}
	if !a.activateFolder(f) {
		return
	}
	for i := len(closing) - 1; i >= 0; i-- {
		a.removeDocument(closing[i])
	}
	for i, oldFolder := range a.folders {
		if oldFolder.path == old {
			a.folders[i] = f
			return
		}
	}
	if old == "" {
		a.folders = nil
	}
	a.folders = append(a.folders, f)
}
func (a *app) removeWorkspaceFolder(f *workspaceFolder) {
	index := -1
	for i, folder := range a.folders {
		if folder == f {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	if a.git.mutating || a.busyAITab() != nil {
		a.notice = "请先结束正在进行的操作并处理待批准修改"
		return
	}
	var closing []int
	for i, d := range a.docs {
		if a.folderForPath(d.path) == f {
			if !a.canClose(d) {
				return
			}
			closing = append(closing, i)
		}
	}
	if a.root == f.path {
		next := &workspaceFolder{}
		for _, folder := range a.folders {
			if folder != f {
				next = folder
				break
			}
		}
		if !a.activateFolder(next) {
			return
		}
		a.closeGit()
	}
	for i := len(closing) - 1; i >= 0; i-- {
		a.removeDocument(closing[i])
	}
	a.folders = append(a.folders[:index], a.folders[index+1:]...)
	a.index = nil
	a.treeSelection = a.root
	a.fileFilter.close()
	a.quick = false
	a.git.visible = false
	a.checkpointWorkspace()
}

func (a *app) folderTree(c *ui.Context, f *workspaceFolder) {
	ui.Column(c.Key(f.path)).FillWidth().Gap(2).Children(func() {
		row := ui.Row(c).FillWidth().Height(30).Padding(0, 6).Gap(6).Radius(6).AlignItems(ui.Center).Label(f.path).Tooltip(f.path).Cursor(ui.CursorPointer).OnClick(func() {
			if f.path == a.root {
				a.treeSelection = f.path
				f.collapsed = !f.collapsed
			} else if a.activateFolder(f) {
				f.collapsed = false
			}
		})
		if len(a.folders) > 1 && a.root == f.path {
			row.Background(c.Theme().Sidebar.Mix(c.Theme().Accent, 0.14))
		}
		row.ContextMenu(func(m *ui.Menu) {
			if m.Item("从当前窗口移除项目").Chosen() {
				a.removeWorkspaceFolder(f)
				return
			}
			m.Separator()
			a.fileMenu(c, f.path, true, true)(m)
		}).Children(func() {
			arrow := "chevron-down"
			if f.collapsed {
				arrow = "chevron-right"
			}
			icon(c, arrow, 10)
			icon(c, "folder", 14)
			ui.Text(c, filepath.Base(f.path)).Bold().FontSize(12).MaxLines(1)
		})
		if !f.collapsed {
			a.tree(c, f, f.entries, 0)
		}
	})
}

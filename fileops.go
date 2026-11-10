package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	golet "github.com/HyperparameterSI/golet/backend"
	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/editor"
	"github.com/HyperparameterSI/golet/backend/ui/forma"
)

type filePrompt struct {
	open                  bool
	kind, path, name, err string
}
type fileClipboard struct {
	path string
	cut  bool
}

func inside(parent, path string) bool {
	rel, e := filepath.Rel(parent, path)
	return e == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}
func namedPath(dir, name string) (string, error) {
	if strings.TrimSpace(name) == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
		return "", errors.New("请输入有效名称，不包含路径分隔符")
	}
	path := filepath.Join(dir, name)
	if _, e := os.Lstat(path); e == nil {
		return "", errors.New("同名文件或文件夹已存在")
	} else if !os.IsNotExist(e) {
		return "", e
	}
	return path, nil
}

// A selected folder is the parent; a selected file creates siblings.
func (a *app) creationDirectory() string {
	path := a.treeSelection
	if path == "" || !inside(a.root, path) {
		return a.root
	}
	info, err := os.Stat(path)
	if err != nil {
		return a.root
	}
	if info.IsDir() {
		return path
	}
	return filepath.Dir(path)
}
func (a *app) askFile(kind, path string) {
	a.filePrompt = filePrompt{open: true, kind: kind, path: path}
	if kind == "重命名" {
		a.filePrompt.name = filepath.Base(path)
	}
}
func (a *app) fileDialog(c *ui.Context) {
	p := &a.filePrompt
	forma.Dialog(c.Key("file-operation"), &p.open, p.kind, func() {
		muted(c, p.path)
		field := forma.Input(c.Key("file-name"), &p.name, forma.InputOptions{Placeholder: "名称"}).FillWidth().AutoFocus()
		if p.err != "" {
			ui.Text(c, p.err).TextColor(c.Theme().Danger)
		}
		submit := field.Submitted()
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			action(c, "cancel-file", "取消", func() { p.open = false })
			if forma.Button(c, "确定").Clicked() {
				submit = true
			}
		})
		if submit {
			if err := a.applyFilePrompt(); err != nil {
				p.err = err.Error()
			} else {
				p.open = false
			}
		}
	})
}
func (a *app) applyFilePrompt() error {
	if a.git.mutating {
		return errors.New("请等待 Git 操作完成")
	}
	p := a.filePrompt
	dir := p.path
	if p.kind == "重命名" {
		dir = filepath.Dir(dir)
		if p.name == filepath.Base(p.path) {
			return nil
		}
	}
	dest, err := namedPath(dir, p.name)
	if err != nil {
		return err
	}
	switch p.kind {
	case "新建文件":
		f, e := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if e != nil {
			return e
		}
		if e = f.Close(); e != nil {
			return e
		}
		a.open(dest)
	case "新建文件夹":
		if err = os.Mkdir(dest, 0755); err != nil {
			return err
		}
	case "重命名":
		if err = a.movePath(p.path, dest); err != nil {
			return err
		}
	default:
		return errors.New("未知文件操作")
	}
	a.treeSelection = dest
	a.expanded[dir] = true
	a.refresh()
	return nil
}
func (a *app) fileMenu(c *ui.Context, path string, dir, root bool) func(*ui.Menu) {
	return func(m *ui.Menu) {
		if !a.activateForPath(path) {
			return
		}
		a.treeSelection = path
		if root || !dir {
			if m.Item("AI 修改备份…").Chosen() {
				a.showAIBackups(path, root)
			}
		}
		if !dir && isWebFile(path) {
			if m.Item("预览").Chosen() {
				a.previewWeb(path)
			}
			m.Separator()
		}
		target := path
		if !dir {
			target = filepath.Dir(path)
		}
		if m.Item("在此目录打开终端").Chosen() {
			a.openTerminal(target, "")
		}
		if m.Item("新建文件…").Chosen() {
			a.askFile("新建文件", target)
		}
		if m.Item("新建文件夹…").Chosen() {
			a.askFile("新建文件夹", target)
		}
		m.Separator()
		if !root {
			if m.Item("剪切").Chosen() {
				a.fileClipboard = fileClipboard{path: path, cut: true}
			}
			if m.Item("复制").Chosen() {
				a.fileClipboard = fileClipboard{path: path}
			}
		}
		if m.Item("粘贴").Disabled(a.fileClipboard.path == "").Chosen() {
			a.report(a.pasteFile(target))
		}
		m.Separator()
		if m.Item("复制绝对路径").Chosen() {
			c.WriteClipboard(path)
		}
		if m.Item("复制相对路径").Chosen() {
			rel, err := filepath.Rel(a.root, path)
			if err == nil {
				c.WriteClipboard(rel)
			}
		}
		if m.Item("在文件管理器中显示").Chosen() {
			golet.Shell.ShowItemInFolder(path)
		}
		if !root {
			m.Separator()
			if m.Item("重命名…").Chosen() {
				a.askFile("重命名", path)
			}
			if m.Item("移到废纸篓").Chosen() {
				a.report(a.trashPath(path, golet.Shell.TrashItem))
			}
		}
		m.Separator()
		if m.Item("刷新").Chosen() {
			a.refresh()
		}
	}
}

// Flush pending edits before moving/copying/removing their on-disk source.
func (a *app) flushPath(path string) error {
	for _, d := range a.docs {
		if d.path != "" && inside(path, d.path) && d.state.Buffer.Modified() {
			if !a.writeAuto(d) {
				return errors.New(d.saveError)
			}
		}
	}
	return nil
}
func (a *app) movePath(src, dst string) error {
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		return errors.New("目标已存在或无法访问")
	}
	if inside(src, dst) {
		return errors.New("不能移动到自身或子文件夹中")
	}
	if err := a.flushPath(src); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	for i, path := range a.recentFiles {
		if inside(src, path) {
			rel, _ := filepath.Rel(src, path)
			a.recentFiles[i] = filepath.Join(dst, rel)
		}
	}
	a.persistRecentFiles()
	if a.treeSelection != "" && inside(src, a.treeSelection) {
		rel, _ := filepath.Rel(src, a.treeSelection)
		a.treeSelection = filepath.Join(dst, rel)
	}
	for _, d := range a.docs {
		if d.path != "" && inside(src, d.path) {
			rel, _ := filepath.Rel(src, d.path)
			d.path = filepath.Join(dst, rel)
			d.name = filepath.Base(d.path)
			d.state.Label = d.name
			if d.editable() {
				d.kind = textFile
				if kindFor(d.name) == markdownFile {
					d.kind = markdownFile
				}
			}
			d.state.Buffer.SetLanguage(editor.LanguageFor(d.path))
		}
	}
	updated := map[string]bool{}
	for path, open := range a.expanded {
		if inside(src, path) {
			rel, _ := filepath.Rel(src, path)
			path = filepath.Join(dst, rel)
		}
		updated[path] = open
	}
	a.expanded = updated
	if a.fileClipboard.path != "" && inside(src, a.fileClipboard.path) {
		rel, _ := filepath.Rel(src, a.fileClipboard.path)
		a.fileClipboard.path = filepath.Join(dst, rel)
	}
	a.refresh()
	return nil
}
func (a *app) pasteFile(dir string) error {
	clip := a.fileClipboard
	if clip.path == "" {
		return nil
	}
	// Resolve destination folders to prevent copying a tree into itself through a symlink.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	dir = resolved
	srcParent, err := filepath.EvalSymlinks(filepath.Dir(clip.path))
	if err != nil {
		return err
	}
	src := filepath.Join(srcParent, filepath.Base(clip.path))
	if inside(src, dir) {
		return errors.New("不能粘贴到自身或子文件夹中")
	}
	dst, err := namedPath(dir, filepath.Base(src))
	if err != nil {
		return err
	}
	if err = a.flushPath(src); err != nil {
		return err
	}
	if clip.cut {
		err = a.movePath(src, dst)
		if err == nil {
			a.fileClipboard = fileClipboard{}
		}
	} else {
		err = copyEntry(src, dst)
	}
	if err != nil {
		return err
	}
	a.expanded[dir] = true
	a.refresh()
	return nil
}

// Copy links as links, never traverse them; do not replace existing targets.
func copyEntry(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, e := os.Readlink(src)
		if e != nil {
			return e
		}
		return os.Symlink(target, dst)
	}
	if info.IsDir() {
		if err = os.Mkdir(dst, info.Mode().Perm()|0700); err != nil {
			return err
		}
		complete := false
		defer func() {
			if !complete {
				_ = os.RemoveAll(dst)
			}
		}()
		entries, e := os.ReadDir(src)
		if e != nil {
			return e
		}
		for _, entry := range entries {
			if e = copyEntry(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); e != nil {
				return e
			}
		}
		if err = os.Chmod(dst, info.Mode().Perm()); err != nil {
			return err
		}
		complete = true
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("不支持复制特殊文件：%s", src)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(dst)
	}
	return err
}
func (a *app) trashPath(path string, trash func(string) error) error {
	if path == a.root {
		return errors.New("不能移除工作区根目录")
	}
	if err := a.flushPath(path); err != nil {
		return err
	}
	if err := trash(path); err != nil {
		return err
	}
	current := a.current()
	kept := a.docs[:0]
	for _, d := range a.docs {
		if d.path == "" || !inside(path, d.path) {
			kept = append(kept, d)
		}
	}
	a.docs = kept
	a.active = -1
	for i, d := range a.docs {
		if d == current {
			a.active = i
		}
	}
	if a.active < 0 && len(a.docs) > 0 {
		a.active = len(a.docs) - 1
	}
	if a.fileClipboard.path != "" && inside(path, a.fileClipboard.path) {
		a.fileClipboard = fileClipboard{}
	}
	a.refresh()
	return nil
}

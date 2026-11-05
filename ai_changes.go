package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

type aiFile struct {
	Data []byte
	Mode fs.FileMode
}
type aiChange struct {
	Path          string
	Before, After *aiFile
}
type aiReview struct {
	Root, Work, Temp string
	Baseline         map[string]aiFile
	Approval         bool
	Changes          []aiChange
}

func snapshotAI(root string) (map[string]aiFile, error) {
	files := map[string]aiFile{}
	var total int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root {
				switch d.Name() {
				case ".git", "node_modules", "vendor", ".idea", "bin", "artifacts", ".venv":
					return filepath.SkipDir
				}
			}
			return nil
		}
		// Never follow links into the original workspace or outside it.
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("审批快照暂不支持符号链接：%s", rel)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("不支持的文件类型：%s", rel)
		}
		total += info.Size()
		if total > 128<<20 {
			return fmt.Errorf("项目快照超过 128 MB，请打开较小的项目目录")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = aiFile{data, info.Mode().Perm()}
		return nil
	})
	return files, err
}
func prepareAIReview(root, temp string, approval bool) (*aiReview, error) {
	baseline, err := snapshotAI(root)
	if err != nil {
		return nil, err
	}
	r := &aiReview{Root: root, Work: root, Temp: temp, Baseline: baseline, Approval: approval}
	if !approval {
		return r, nil
	}
	r.Work = filepath.Join(temp, "workspace")
	if err = os.MkdirAll(r.Work, 0700); err != nil {
		return nil, err
	}
	for path, f := range baseline {
		target := filepath.Join(r.Work, path)
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return nil, err
		}
		if err = os.WriteFile(target, f.Data, f.Mode); err != nil {
			return nil, err
		}
	}
	return r, nil
}
func (r *aiReview) scan() ([]aiChange, error) {
	current, err := snapshotAI(r.Work)
	if err != nil {
		return nil, err
	}
	var changes []aiChange
	for path, before := range r.Baseline {
		after, ok := current[path]
		if !ok {
			b := before
			changes = append(changes, aiChange{Path: path, Before: &b})
			continue
		}
		if !bytes.Equal(before.Data, after.Data) || before.Mode != after.Mode {
			b, a := before, after
			changes = append(changes, aiChange{path, &b, &a})
		}
	}
	for path, after := range current {
		if _, ok := r.Baseline[path]; !ok {
			a := after
			changes = append(changes, aiChange{Path: path, After: &a})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}
func (change aiChange) label() string {
	action := "修改"
	if change.Before == nil {
		action = "新建"
	}
	if change.After == nil {
		action = "删除"
	}
	line := 1
	if change.Before != nil && change.After != nil {
		a, b := strings.Split(string(change.Before.Data), "\n"), strings.Split(string(change.After.Data), "\n")
		for line <= len(a) && line <= len(b) && a[line-1] == b[line-1] {
			line++
		}
	}
	return fmt.Sprintf("%s · %s:%d", action, change.Path, line)
}
func diffAIText(f *aiFile) string {
	if f == nil {
		return ""
	}
	if !utf8.Valid(f.Data) || bytes.ContainsRune(f.Data, 0) {
		return fmt.Sprintf("[二进制文件 · %d 字节]", len(f.Data))
	}
	return string(f.Data)
}
func checkAIChange(root string, c aiChange) error {
	path := filepath.Join(root, c.Path)
	if !inside(root, path) || c.Path == "." {
		return fmt.Errorf("无效的修改路径")
	}
	// Reject redirected parents and leaf links, including links introduced after scan.
	for part := path; part != root; part = filepath.Dir(part) {
		info, err := os.Lstat(part)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("路径已变为符号链接：%s", c.Path)
		}
	}
	info, err := os.Lstat(path)
	if c.Before == nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("文件已存在：%s", c.Path)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("文件已变化：%s", c.Path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("文件类型已变化：%s", c.Path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(data, c.Before.Data) || info.Mode().Perm() != c.Before.Mode {
		return fmt.Errorf("文件已被其他操作修改，请重新生成：%s", c.Path)
	}
	return nil
}
func applyAIChange(root string, c aiChange) error {
	if err := checkAIChange(root, c); err != nil {
		return err
	}
	path := filepath.Join(root, c.Path)
	if c.After == nil {
		return os.Remove(path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".omnicode-apply-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(c.After.Data); err != nil {
		f.Close()
		return err
	}
	if err = f.Chmod(c.After.Mode); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

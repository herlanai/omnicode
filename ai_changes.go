package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
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
	Baseline         map[string]aiBaseline
	Links            map[string]bool
	Approval         bool
	Changes          []aiChange
}

type aiBaseline struct {
	hash     [sha256.Size]byte
	size     int64
	modified time.Time
	mode     fs.FileMode
}

// Walk metadata first. Only the visitor decides whether file contents are needed.
func walkAIFiles(root string, links map[string]bool, visit func(*os.Root, string, fs.FileInfo) error) error {
	tree, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer tree.Close()
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
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
		if d.Type()&os.ModeSymlink != 0 {
			if rel == "." {
				return fmt.Errorf("项目根目录已变为符号链接")
			}
			links[rel] = true
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("不支持的文件类型：%s", rel)
		}
		return visit(tree, rel, info)
	})
}

func copyAIFile(target string, src io.Reader, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, src)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func prepareAIReview(root, temp string, approval bool) (*aiReview, error) {
	if inside(root, temp) {
		return nil, fmt.Errorf("AI 临时目录必须位于项目之外")
	}
	links := map[string]bool{}
	r := &aiReview{Root: root, Work: root, Temp: temp, Approval: approval, Links: links, Baseline: map[string]aiBaseline{}}
	if approval {
		r.Work = filepath.Join(temp, "workspace")
		if err := os.MkdirAll(r.Work, 0700); err != nil {
			return nil, err
		}
	}
	err := walkAIFiles(root, links, func(tree *os.Root, path string, info fs.FileInfo) error {
		src, err := tree.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		baseline := filepath.Join(temp, "baseline", path)
		h := sha256.New()
		if err := copyAIFile(baseline, io.TeeReader(src, h), 0600); err != nil {
			return err
		}
		mode := info.Mode().Perm()
		if approval {
			f, err := os.Open(baseline)
			if err != nil {
				return err
			}
			defer f.Close()
			work := filepath.Join(r.Work, path)
			if err := copyAIFile(work, f, mode); err != nil {
				return err
			}
			if err := os.Chmod(work, mode); err != nil {
				return err
			}
			info, err = os.Stat(work)
			if err != nil {
				return err
			}
		}
		r.Baseline[path] = aiBaseline{[32]byte(h.Sum(nil)), info.Size(), info.ModTime(), mode}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !approval {
		return r, nil
	}
	// Rebase internal aliases onto files and directories actually present in the copy.
	// Never recreate a link with its original absolute destination.
	if len(links) > 0 {
		canonical, err := filepath.EvalSymlinks(root)
		if err != nil {
			return nil, err
		}
		for path := range links {
			resolved, err := filepath.EvalSymlinks(filepath.Join(canonical, path))
			if err != nil || resolved == canonical || !inside(canonical, resolved) {
				continue
			}
			rel, err := filepath.Rel(canonical, resolved)
			if err != nil {
				return nil, err
			}
			dest := filepath.Join(r.Work, rel)
			if _, err = os.Lstat(dest); os.IsNotExist(err) {
				continue
			} else if err != nil {
				return nil, err
			}
			alias := filepath.Join(r.Work, path)
			dir := filepath.Dir(alias)
			target, err := filepath.Rel(dir, dest)
			if err != nil {
				return nil, err
			}
			if err = os.MkdirAll(dir, 0755); err != nil {
				return nil, err
			}
			if err = os.Symlink(target, alias); err != nil {
				return nil, err
			}
		}
	}

	return r, nil
}
func (r *aiReview) scan() ([]aiChange, error) {
	return r.scanChanges(true)
}

func (r *aiReview) scanChanges(final bool) ([]aiChange, error) {
	links := map[string]bool{}
	seen := map[string]bool{}
	var changes []aiChange
	beforeFile := func(path string, b aiBaseline) (*aiFile, error) {
		data, err := os.ReadFile(filepath.Join(r.Temp, "baseline", path))
		if err != nil {
			return nil, err
		}
		if sha256.Sum256(data) != b.hash {
			return nil, fmt.Errorf("原始版本校验失败：%s", path)
		}
		return &aiFile{data, b.mode}, nil
	}
	err := walkAIFiles(r.Work, links, func(tree *os.Root, path string, info fs.FileInfo) error {
		seen[path] = true
		b, exists := r.Baseline[path]
		mode := info.Mode().Perm()
		if !exists && underAILink(path, r.Links) {
			return nil
		}
		if exists && mode == b.mode && info.Size() == b.size {
			if !final && info.ModTime().Equal(b.modified) {
				return nil
			}
			f, err := tree.Open(path)
			if err != nil {
				return err
			}
			h := sha256.New()
			_, err = io.Copy(h, f)
			f.Close()
			if err != nil {
				return err
			}
			if [32]byte(h.Sum(nil)) == b.hash {
				return nil
			}
		}
		data, err := tree.ReadFile(path)
		if err != nil {
			return err
		}
		change := aiChange{Path: path, After: &aiFile{data, mode}}
		if exists {
			change.Before, err = beforeFile(path, b)
			if err != nil {
				return err
			}
		}
		changes = append(changes, change)
		return nil
	})
	if err != nil {
		return nil, err
	}
	for path, b := range r.Baseline {
		if !seen[path] && !underAILink(path, links) {
			before, err := beforeFile(path, b)
			if err != nil {
				return nil, err
			}
			changes = append(changes, aiChange{Path: path, Before: before})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}

// Link replacements are outside regular-file review, including their descendants.
func underAILink(path string, links map[string]bool) bool {
	for part := path; part != "."; part = filepath.Dir(part) {
		if links[part] {
			return true
		}
	}
	return false
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

package main

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/forma"
)

const explorerMatchLimit = 100

type explorerFilter struct {
	open, focus     bool
	query, searched string
	matches         []string
	pending         <-chan []string
	cancel          context.CancelFunc
}

func (f *explorerFilter) resetSearch() {
	if f.cancel != nil {
		f.cancel()
	}
	f.pending = nil
	f.cancel = nil
	f.matches = nil
	f.searched = ""
}
func (f *explorerFilter) close() { f.resetSearch(); *f = explorerFilter{} }

// Use the same hidden entries as the explorer and never traverse directory links.
func findExplorerFiles(ctx context.Context, roots []string, query string) []string {
	var matches []string
	seen := map[string]bool{}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return filepath.SkipAll
			}
			if err != nil || path == root {
				return nil
			}
			name := d.Name()
			if name == ".git" || name == ".DS_Store" {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.IsDir() && strings.Contains(strings.ToLower(name), query) {
				if !seen[path] {
					matches = append(matches, path)
					seen[path] = true
				}
				if len(matches) > explorerMatchLimit {
					return filepath.SkipAll
				}
			}
			return nil
		})
		if ctx.Err() != nil || len(matches) > explorerMatchLimit {
			break
		}
	}
	return matches
}
func (a *app) explorerFilterView(c *ui.Context) bool {
	f := &a.fileFilter
	input := forma.Input(c.Key("explorer-filter"), &f.query, forma.InputOptions{Placeholder: "按文件名筛选…"}).FillWidth().Label("文件名筛选")
	if f.focus {
		input.Focus()
		f.focus = false
	}
	if input.Focused() && c.Shortcut(0, ui.KeyEscape) {
		f.close()
		return false
	}
	q := strings.ToLower(strings.TrimSpace(f.query))
	if q != f.searched {
		f.resetSearch()
		if q != "" {
			f.searched = q
			ctx, cancel := context.WithCancel(context.Background())
			f.cancel = cancel
			result := make(chan []string, 1)
			f.pending = result
			var roots []string
			for _, f := range a.folders {
				roots = append(roots, f.path)
			}
			go func() { result <- findExplorerFiles(ctx, roots, q) }()
		}
	}
	if q == "" {
		return false
	}
	if f.pending != nil {
		select {
		case f.matches = <-f.pending:
			f.pending = nil
			f.cancel()
			f.cancel = nil
		default:
			muted(c, "正在筛选…")
			c.After(40 * time.Millisecond)
			return true
		}
	}
	if len(f.matches) == 0 {
		muted(c, "没有匹配的文件")
		return true
	}
	open := func(path string) {
		if a.activateForPath(path) {
			a.treeSelection = path
			a.open(path)
		}
	}
	if input.Submitted() {
		open(f.matches[0])
	}
	ui.Scroll(c.Key("filtered-files")).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Gap(2).Children(func() {
			for _, path := range f.matches[:min(explorerMatchLimit, len(f.matches))] {
				owner := a.folderForPath(path)
				if owner == nil {
					continue
				}
				rel, _ := filepath.Rel(owner.path, path)
				if len(a.folders) > 1 {
					rel = filepath.Join(filepath.Base(owner.path), rel)
				}
				row := ui.ButtonBase(c.Key(path)).FillWidth().Padding(5, 8).Radius(6).Label(rel).Tooltip(rel).OnClick(func() { open(path) })
				if path == a.treeSelection {
					row.Background(a.theme.Sidebar.Mix(a.theme.Accent, 0.14))
				} else if row.Hovered() {
					row.Background(a.theme.SurfaceHover)
				}
				row.Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(7).Children(func() {
						icon(c, "file", 14)
						ui.Column(c).MinWidth(0).Grow(1).Children(func() {
							ui.Text(c, filepath.Base(rel)).MaxLines(1)
							if dir := filepath.Dir(rel); dir != "." {
								muted(c, dir).MaxLines(1)
							}
						})
					})
				})
			}
			if len(f.matches) > explorerMatchLimit {
				muted(c, "已显示前 100 个匹配文件，请细化文件名").Wrap()
			}
		})
	})
	return true
}

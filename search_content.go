package main

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
)

type contentHit struct {
	Path, Snippet, Match string
	Line, Start, End     int
}
type contentResult struct {
	Hits    []contentHit
	Limited bool
}
type contentSearch struct {
	query, root   string
	hits          []contentHit
	busy, limited bool
	cancel        context.CancelFunc
	results       chan contentResult
}

func searchContents(ctx context.Context, root, query string, open map[string]string) contentResult {
	result := contentResult{}
	if query == "" {
		return result
	}
	pattern := regexp.MustCompile("(?i)" + regexp.QuoteMeta(query))
	var total int
	for _, path := range fileIndex(root) {
		select {
		case <-ctx.Done():
			result.Limited = true
			return result
		default:
		}
		text, ok := open[filepath.Join(root, path)]
		if !ok {
			data, err := readText(filepath.Join(root, path))
			if err != nil {
				continue
			}
			text = string(data)
		}
		total += len(text)
		if total > 128<<20 {
			result.Limited = true
			return result
		}
		offset := 0
		for line, part := range strings.Split(text, "\n") {
			select {
			case <-ctx.Done():
				result.Limited = true
				return result
			default:
			}
			for _, match := range pattern.FindAllStringIndex(part, 101-len(result.Hits)) {
				if len(result.Hits) == 100 {
					result.Limited = true
					return result
				}
				left := []rune(part[:match[0]])
				right := []rune(part[match[0]:])
				snippet := ""
				if len(left) > 45 {
					snippet = "…"
					left = left[len(left)-45:]
				}
				if len(right) > 130 {
					right = append(right[:130], '…')
				}
				snippet += string(left) + string(right)
				result.Hits = append(result.Hits, contentHit{Path: path, Snippet: strings.TrimSpace(snippet), Match: part[match[0]:match[1]], Line: line + 1, Start: offset + match[0], End: offset + match[1]})
			}
			offset += len(part) + 1
		}
	}
	return result
}
func (a *app) updateContentSearch(c *ui.Context) {
	s := &a.contentSearch
	if s.query != a.query || s.root != a.root {
		if s.cancel != nil {
			s.cancel()
		}
		*s = contentSearch{query: a.query, root: a.root}
		if a.query != "" && a.root != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			s.cancel = cancel
			s.busy = true
			s.results = make(chan contentResult, 1)
			query, root, results := s.query, s.root, s.results
			open := map[string]string{}
			for _, d := range a.docs {
				if d.editable() && d.path != "" {
					open[d.path] = d.state.Buffer.String()
				}
			}
			go func() {
				defer cancel()
				timer := time.NewTimer(180 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-ctx.Done():
					return
				}
				results <- searchContents(ctx, root, query, open)
			}()
		}
	}
	if s.busy {
		select {
		case result := <-s.results:
			s.hits = result.Hits
			s.limited = result.Limited
			s.busy = false
		default:
			c.After(50 * time.Millisecond)
		}
	}
}
func (a *app) openContentHit(hit contentHit) {
	a.open(filepath.Join(a.root, hit.Path))
	d := a.current()
	if d == nil || !d.editable() || d.path != filepath.Join(a.root, hit.Path) {
		return
	}
	text := d.state.Buffer.String()
	start, end := hit.Start, hit.End
	if start < 0 || end > len(text) || text[start:end] != hit.Match {
		start = strings.Index(text, hit.Match)
		if start < 0 {
			a.notice = "文件内容已更新，请重新搜索"
			return
		}
		end = start + len(hit.Match)
	}
	d.state.Select(start, end)
	d.state.ScrollTo(strings.Count(text[:start], "\n"))
	a.focusEditor = true
}
func contentHitLabel(hit contentHit) string { return fmt.Sprintf("%s:%d", hit.Path, hit.Line) }

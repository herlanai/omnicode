package main

import (
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
)

type explorerUpdate struct {
	generation  uint64
	directories map[string][]*entry
}

type explorerRefresh struct {
	next       time.Time
	generation uint64
	pending    <-chan explorerUpdate
}

func mergeExplorerEntries(old, fresh []*entry) ([]*entry, bool) {
	changed := len(old) != len(fresh)
	existing := make(map[string]*entry, len(old))
	for _, e := range old {
		existing[e.path] = e
	}
	for i, e := range fresh {
		if i >= len(old) || old[i].path != e.path || old[i].dir != e.dir {
			changed = true
		}
		if prev := existing[e.path]; prev != nil && prev.dir == e.dir {
			fresh[i] = prev
		}
	}
	return fresh, changed
}

func (a *app) tickExplorer(c *ui.Context) {
	if len(a.folders) == 0 {
		return
	}
	p := &a.explorerRefresh
	c.After(500 * time.Millisecond)
	if p.pending != nil {
		select {
		case result := <-p.pending:
			p.pending = nil
			if result.generation != p.generation {
				p.next = time.Time{}
				return
			}
			changed := false
			var apply func(string, []*entry) []*entry
			apply = func(path string, old []*entry) []*entry {
				if fresh, ok := result.directories[path]; ok {
					var diff bool
					old, diff = mergeExplorerEntries(old, fresh)
					changed = changed || diff
				}
				for _, e := range old {
					if e.loaded {
						e.children = apply(e.path, e.children)
					}
				}
				return old
			}
			for _, f := range a.folders {
				f.entries = apply(f.path, f.entries)
			}
			if changed {
				a.index = nil
				a.fileFilter.resetSearch()
			}
		default:
			return
		}
	}
	if time.Now().Before(p.next) {
		return
	}
	var paths []string
	var collect func([]*entry)
	collect = func(entries []*entry) {
		for _, e := range entries {
			if e.loaded {
				paths = append(paths, e.path)
				collect(e.children)
			}
		}
	}
	for _, f := range a.folders {
		paths = append(paths, f.path)
		collect(f.entries)
	}
	result := make(chan explorerUpdate, 1)
	p.pending = result
	p.next = time.Now().Add(500 * time.Millisecond)
	generation := p.generation
	go func() {
		update := explorerUpdate{generation: generation, directories: map[string][]*entry{}}
		for _, path := range paths {
			if list, err := children(path); err == nil {
				update.directories[path] = list
			}
		}
		result <- update
	}()
}

package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestRecentDockLimitAndOpen(t *testing.T) {
	var projects []recentProject
	for i := 0; i < 12; i++ {
		projects = append(projects, recentProject{Path: fmt.Sprintf("/projects/project-%d", i)})
	}
	opened := ""
	items := recentDockItems(projects, func(path string) { opened = path })
	if len(items) != 10 {
		t.Fatalf("got %d entries", len(items))
	}
	for i, item := range items {
		if !strings.Contains(item.Label, fmt.Sprintf("project-%d", i)) {
			t.Fatal("order changed", item.Label)
		}
		item.Click(nil, nil)
		if opened != projects[i].Path {
			t.Fatalf("wrong project: %s", opened)
		}
	}
	if len(recentDockItems(nil, nil)) != 0 {
		t.Fatal("empty history must have no project entries")
	}
}

func TestRecentDockDuplicateNames(t *testing.T) {
	items := recentDockItems([]recentProject{{Path: "/a/project"}, {Path: "/b/project"}}, func(string) {})
	if items[0].Subtitle == "" || items[0].Subtitle == items[1].Subtitle {
		t.Fatal("same-name projects must be distinguishable")
	}
}

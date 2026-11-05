package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/HyperparameterSI/golet/backend/ui"
)

func TestRecentProjectsPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.sqlite")
	s, e := openProjectStore(path)
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{"/projects/alpha", "/projects/带空格 beta", "/projects/alpha"} {
		if e = s.remember(p); e != nil {
			t.Fatal(e)
		}
	}
	s.db.Close()
	s, e = openProjectStore(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	list, e := s.list()
	if e != nil {
		t.Fatal(e)
	}
	if len(list) != 2 || list[0].Path != "/projects/alpha" {
		t.Fatalf("incorrect persistent recents: %+v", list)
	}
	if e = s.remove(list[0].Path); e != nil {
		t.Fatal(e)
	}
	list, _ = s.list()
	if len(list) != 1 {
		t.Fatal("remove failed")
	}
	for i := 0; i < 24; i++ {
		if e = s.remember(fmt.Sprintf("/projects/%d", i)); e != nil {
			t.Fatal(e)
		}
	}
	list, _ = s.list()
	if len(list) != 20 || list[0].Path != "/projects/23" {
		t.Fatal("history bound/order failed")
	}
}
func TestRecentProjectsView(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "hello-project")
	os.Mkdir(project, 0755)
	s, e := openProjectStore(filepath.Join(root, "cache.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	a := newApp("")
	a.projects = s
	a.setRoot(project)
	if len(a.recent) != 1 {
		t.Fatal("opening folder did not record it")
	}
	old := a.root
	a.setRoot(filepath.Join(root, "missing"))
	if a.root != old || len(a.recent) != 1 {
		t.Fatal("invalid path polluted history")
	}
	a.root = ""
	a.notice = ""
	tester := ui.NewTester(a.view, 1200, 780)
	if !tester.HasText("最近项目") {
		t.Fatal("recent section missing")
	}
	if dir := os.Getenv("OMNICODE_VIEWER_ARTIFACTS"); dir != "" {
		os.MkdirAll(dir, 0755)
		f, e := os.Create(filepath.Join(dir, "recent-projects.png"))
		if e != nil {
			t.Fatal(e)
		}
		png.Encode(f, tester.Image())
		f.Close()
	}
	if e = tester.Click("打开项目 " + old); e != nil {
		t.Fatal(e)
	}
	if a.root != old {
		t.Fatal("recent project did not open")
	}
	if e = tester.Click("移除最近项目记录"); e != nil {
		t.Fatal(e)
	}
	if len(a.recent) != 0 {
		t.Fatal("remove action failed")
	}
	if _, e = os.Stat(project); e != nil {
		t.Fatal("removing history affected project")
	}
}

package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
)

func TestDatabaseSettingsMultipleConnections(t *testing.T) {
	store, err := openProjectStore(filepath.Join(t.TempDir(), "settings.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()
	first, second := testDB(t), testDB(t)
	first.Name = "本地开发"
	first.ID = "first"
	second.Name = "测试数据"
	second.ID = ""
	a := newApp("")
	a.projects = store
	a.database.connections = []dbConnection{first}
	if !a.saveDatabasePanel() {
		t.Fatal(a.database.err)
	}
	a.database.connections = nil
	a.database.loaded = false
	a.showSettings()
	v := ui.NewTester(a.view, 1200, 850)
	if err = v.Click("数据库"); err != nil {
		t.Fatal(err)
	}
	if !v.HasText("本地开发") {
		t.Fatal("saved connection not loaded")
	}
	if err = v.Click("新增数据库连接"); err != nil {
		t.Fatal(err)
	}
	*a.database.draft = second
	v.Frame()
	if err = v.Click("保存连接"); err != nil {
		t.Fatal(err)
	}
	if !a.settingsOpen || a.database.open || a.database.busy || len(a.database.connections) != 2 || a.database.draft != nil {
		t.Fatal("settings save should return to shared list without connecting")
	}
	if err = v.Click("编辑连接 本地开发"); err != nil {
		t.Fatal(err)
	}
	a.database.draft.Name = "已更新开发库"
	v.Frame()
	if err = v.Click("保存连接"); err != nil {
		t.Fatal(err)
	}
	if len(a.database.connections) != 2 || a.database.connections[0].ID != "first" {
		t.Fatal("editing duplicated the connection")
	}
	if err = v.Click("测试连接 测试数据"); err != nil {
		t.Fatal(err)
	}
	if a.database.busy {
		select {
		case ev := <-a.database.done:
			a.database.done <- ev
			v.Frame()
		case <-time.After(5 * time.Second):
			t.Fatal("test connection timed out")
		}
	}

	if a.database.err != "" || !v.HasText("连接成功") {
		t.Fatal(a.database.err)
	}
	v.Advance(200 * time.Millisecond)
	if os.Getenv("OMNICODE_DATABASE_SCREENSHOT") != "" {
		os.MkdirAll("artifacts", 0755)
		f, e := os.Create("artifacts/database-settings.png")
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		if e = png.Encode(f, v.Image()); e != nil {
			t.Fatal(e)
		}
	}
	b := newApp("")
	b.projects = store
	b.openDatabasePanel()
	if len(b.database.connections) != 2 || b.database.connections[0].Name != "已更新开发库" {
		t.Fatal("settings changes not shared with database tool")
	}
	if err = v.Click("数据库管理"); err != nil {
		t.Fatal(err)
	}
	if a.settingsOpen || !a.database.open || len(a.database.connections) != 2 {
		t.Fatal("database tool transition lost connections")
	}
}

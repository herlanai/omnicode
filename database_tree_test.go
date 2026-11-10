package main

import (
	"context"
	"testing"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
)

func TestDatabaseConnectionTree(t *testing.T) {
	first, second := testDB(t), testDB(t)
	first.ID, first.Name = "first", "开发数据库"
	second.ID, second.Name = "second", "测试数据库"
	for _, item := range []struct {
		conn dbConnection
		name string
	}{{first, "first_table"}, {second, "second_table"}} {
		if _, err := dbRun(context.Background(), item.conn, "CREATE TABLE "+item.name+" (id INTEGER)", nil, true); err != nil {
			t.Fatal(err)
		}
	}
	a := newApp("")
	a.openDatabasePanel()
	p := &a.database
	p.connections = []dbConnection{first, second}
	v := ui.NewTester(a.view, 1100, 800)
	settle := func() {
		t.Helper()
		if p.busy {
			select {
			case event := <-p.done:
				p.done <- event
				v.Frame()
			case <-time.After(5 * time.Second):
				t.Fatal("database task timed out")
			}
		}
		if p.err != "" {
			t.Fatal(p.err)
		}
	}
	click := func(label string) {
		t.Helper()
		if err := v.Click(label); err != nil {
			t.Fatal(err)
		}
		settle()
	}
	click(first.Name)
	if !v.HasText("first_table") || v.HasText("second_table") {
		t.Fatal("first connection tables missing or mixed")
	}
	click(second.Name)
	if !v.HasText("first_table") || !v.HasText("second_table") {
		t.Fatal("connections cannot expand independently")
	}
	click(first.Name)
	if v.HasText("first_table") || !v.HasText("second_table") {
		t.Fatal("collapse affected other connection")
	}
	click(first.Name)
	if !v.HasText("first_table") {
		t.Fatal("cached tables missing after expansion")
	}
	click("second_table")
	if p.active != second.ID || p.table != "second_table" {
		t.Fatalf("table query used wrong connection: active=%s table=%s busy=%v", p.active, p.table, p.busy)
	}
	click(second.Name)
	if p.tree(second.ID).expanded || !v.HasText("first_table") {
		t.Fatal("second connection did not collapse")
	}
}

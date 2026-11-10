package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
)

func TestDatabaseCreateSQL(t *testing.T) {
	conn := testDB(t)
	ctx := context.Background()
	table := "odd\x27table"
	ddl := "CREATE TABLE " + dbQuote(table) + " (id INTEGER PRIMARY KEY, value TEXT DEFAULT 'hello', UNIQUE(value))"
	if _, err := dbRun(ctx, conn, ddl, nil, true); err != nil {
		t.Fatal(err)
	}
	got, err := dbCreateSQL(ctx, conn, table)
	if err != nil || got != ddl {
		t.Fatalf("DDL lost original constraints: %q %v", got, err)
	}
	view := "CREATE VIEW test_view AS SELECT * FROM " + dbQuote(table)
	if _, err := dbRun(ctx, conn, view, nil, true); err != nil {
		t.Fatal(err)
	}
	if got, err = dbCreateSQL(ctx, conn, "test_view"); err != nil || got != view {
		t.Fatalf("view: %q %v", got, err)
	}
	if _, err = dbCreateSQL(ctx, conn, "missing"); err == nil {
		t.Fatal("missing table accepted")
	}

	a := newApp("")
	a.openDatabasePanel()
	p := &a.database
	p.connections = []dbConnection{conn}
	p.active = conn.ID
	a.browseDatabase(table, 0, true)
	select {
	case event := <-p.done:
		p.done <- event
	case <-time.After(5 * time.Second):
		t.Fatal("structure timed out")
	}
	v := ui.NewTester(a.view, 1100, 800)
	if p.err != "" || p.structureTable != table || len(p.result.Rows) != 2 || p.ddl == nil {
		t.Fatalf("structure failed: %s", p.err)
	}
	if err = v.Click("创建 SQL"); err != nil {
		t.Fatal(err)
	}
	if !p.showDDL || !p.ddl.ReadOnly || !strings.Contains(p.ddl.Buffer.String(), "UNIQUE(value)") {
		t.Fatal("DDL preview incomplete or editable")
	}
	if err = v.Click("字段"); err != nil {
		t.Fatal(err)
	}
	if p.showDDL || len(p.result.Rows) != 2 {
		t.Fatal("switch lost columns")
	}
	a.selectDatabase(conn.ID)
	if p.structureTable != "" || p.ddl != nil {
		t.Fatal("stale DDL retained")
	}
	if p.cancel != nil {
		p.cancel()
	}
}

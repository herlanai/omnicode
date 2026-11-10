package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HyperparameterSI/golet/backend/plugins/sqlite"
	"github.com/HyperparameterSI/golet/backend/ui"
)

func testDB(t *testing.T) dbConnection {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.sqlite")
	db, e := sqlite.Open(context.Background(), path, sqlite.OpenOptions{})
	if e != nil {
		t.Fatal(e)
	}
	db.Close()
	return dbConnection{ID: "test", Name: "测试数据库", Engine: "SQLite", Path: path}
}
func TestDatabaseSQLiteLifecycle(t *testing.T) {
	c := testDB(t)
	ctx := context.Background()
	for _, statement := range []string{"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, note TEXT)", "INSERT INTO users(name) VALUES ('Alice'),('Bob')"} {
		if _, e := dbRun(ctx, c, statement, nil, true); e != nil {
			t.Fatal(e)
		}
	}
	r, e := dbRun(ctx, c, "SELECT id, name, note FROM users; -- trailing comment", nil, false)
	if e != nil || len(r.Rows) != 2 || r.Rows[0][2] != nil {
		t.Fatalf("%+v %v", r, e)
	}
	r, e = dbRun(ctx, c, dbTablesSQL(c), nil, false)
	if e != nil || len(r.Rows) != 1 || r.Rows[0][0] != "users" {
		t.Fatalf("%+v %v", r, e)
	}
	q, args := dbStructureSQL(c, "users")
	r, e = dbRun(ctx, c, q, args, false)
	if e != nil || len(r.Rows) != 3 {
		t.Fatalf("%+v %v", r, e)
	}
	if _, e = dbRun(ctx, c, "DELETE FROM users", nil, false); e == nil {
		t.Fatal("write accepted in query mode")
	}
	r, e = dbRun(ctx, c, "UPDATE users SET name=? WHERE id=?", []any{"Changed", 1}, true)
	if e != nil || r.Affected != 1 {
		t.Fatalf("%+v %v", r, e)
	}
	r, e = dbRun(ctx, c, "WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<2000) SELECT x FROM n", nil, false)
	if e != nil || len(r.Rows) != 1000 || !r.Truncated {
		t.Fatalf("limit failed: %d %v", len(r.Rows), e)
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = dbRun(ctx, c, "SELECT 1", nil, false); e == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestDatabaseSingleStatement(t *testing.T) {
	for _, engine := range []string{"SQLite", "MySQL", "Cloudflare D1"} {
		for _, s := range []string{"SELECT 1; DELETE FROM users", "SELECT /*! DELETE */ 1", "SELECT 'unterminated"} {
			if _, _, e := dbSQL(s, engine); e == nil {
				t.Fatal("accepted", engine, s)
			}
		}
		for _, s := range []string{"SELECT ';'", "SELECT 'it''s a test'; -- end", "/*comment*/ SELECT 1"} {
			if _, _, e := dbSQL(s, engine); e != nil {
				t.Fatal(engine, s, e)
			}
		}
	}
	cfg := dbMySQLConfig(dbConnection{Host: "::1", Port: "3306", User: "user", Password: "p@ss", Database: "db", TLS: true})
	if cfg.MultiStatements || cfg.AllowAllFiles || cfg.TLSConfig != "true" || cfg.Addr != "[::1]:3306" {
		t.Fatal("bad mysql configuration")
	}
}

type dbRoundTrip func(*http.Request) (*http.Response, error)

func (f dbRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestDatabaseD1(t *testing.T) {
	conn := dbConnection{AccountID: "account", DatabaseID: "database", Token: "secret"}
	client := &http.Client{Transport: dbRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.cloudflare.com/client/v4/accounts/account/d1/database/database/raw" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("wrong request")
		}
		var body struct {
			SQL    string
			Params []any
		}
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil || body.SQL != "SELECT ?" || len(body.Params) != 1 {
			t.Error("bad request payload")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"success":true,"result":[{"success":true,"results":{"columns":["id","value"],"rows":[[9007199254740993,null]]},"meta":{"changes":0}}]}`))}, nil
	})}
	result, e := dbD1(context.Background(), client, conn, "SELECT ?", []any{1})
	if e != nil || fmt.Sprint(result.Rows[0][0]) != "9007199254740993" || result.Rows[0][1] != nil {
		t.Fatalf("%+v %v", result, e)
	}
	client.Transport = dbRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`{"success":false,"errors":[{"message":"permission denied"}]}`))}, nil
	})
	if _, e = dbD1(context.Background(), client, conn, "SELECT 1", nil); e == nil || !strings.Contains(e.Error(), "permission denied") {
		t.Fatal(e)
	}
}
func TestDatabasePanelNative(t *testing.T) {
	conn := testDB(t)
	ctx := context.Background()
	dbRun(ctx, conn, "CREATE TABLE users(id INTEGER PRIMARY KEY, name TEXT)", nil, true)
	dbRun(ctx, conn, "INSERT INTO users(name) VALUES ('Alice'), ('Bob')", nil, true)
	a := newApp("")
	store, e := openProjectStore(filepath.Join(t.TempDir(), "settings.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer store.db.Close()
	a.projects = store
	view := ui.NewTester(a.view, 1440, 900)
	if e = view.Click("数据库管理"); e != nil {
		t.Fatal(e)
	}
	a.database.connections = []dbConnection{conn}
	if !a.saveDatabasePanel() {
		t.Fatal(a.database.err)
	}
	b := newApp("")
	b.projects = store
	b.openDatabasePanel()
	if len(b.database.connections) != 1 || b.database.connections[0].Path != conn.Path {
		t.Fatal("connection not persisted")
	}
	a.selectDatabase(conn.ID)
	wait := func() {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for a.database.busy {
			select {
			case ev := <-a.database.done:
				a.database.done <- ev
				view.Frame()
			case <-deadline:
				t.Fatal("database job did not complete")
			}
		}
	}
	wait()
	if a.database.err != "" {
		t.Fatal(a.database.err)
	}
	a.browseDatabase("users", 0, false)
	wait()
	if len(a.database.result.Rows) != 2 {
		t.Fatal(a.database.err)
	}
	if _, ok := view.Find("HTTP 请求"); !ok {
		t.Fatal("rail disappeared")
	}
	if os.Getenv("OMNICODE_DATABASE_SCREENSHOT") != "" {
		os.MkdirAll("artifacts", 0755)
		f, e := os.Create("artifacts/database.png")
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		png.Encode(f, view.Image())
	}
	if e = view.Click("新建数据库连接"); e != nil {
		t.Fatal(e)
	}
	if _, ok := view.Find("连接名称"); !ok {
		t.Fatal("missing native connection form")
	}
	a.database.draft.Engine = "Cloudflare D1"
	view.Frame()
	for _, label := range []string{"Account ID", "Database ID", "API Token"} {
		if _, ok := view.Find(label); !ok {
			t.Fatalf("missing D1 field %s", label)
		}
	}
	if os.Getenv("OMNICODE_DATABASE_SCREENSHOT") != "" {
		f, e := os.Create("artifacts/database-d1.png")
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		png.Encode(f, view.Image())
	}
	if e = view.Click("文件浏览器"); e != nil {
		t.Fatal(e)
	}
	if a.database.open {
		t.Fatal("failed to leave database page")
	}
}

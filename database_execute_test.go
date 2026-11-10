package main

import (
	"context"
	"testing"
	"time"
)

func TestDatabaseUnifiedExecution(t *testing.T) {
	for _, tc := range []struct {
		name, sql             string
		approve, confirm, run bool
	}{
		{"select", "SELECT 1", false, false, true},
		{"cte", "WITH q AS (SELECT 1) SELECT * FROM q", false, false, true},
		{"cancel", "CREATE TABLE example(id INTEGER)", false, true, false},
		{"approve", "CREATE TABLE example(id INTEGER)", true, true, true},
		{"multiple", "SELECT 1; DELETE FROM example", true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := testDB(t)
			a := newApp("")
			a.openDatabasePanel()
			a.database.connections = []dbConnection{conn}
			a.database.active = conn.ID
			a.database.sql.Buffer.Set(tc.sql)
			called := false
			a.executeDatabaseSQL(func(c dbConnection, sql string) (bool, error) {
				called = true
				if c.ID != conn.ID || sql != tc.sql {
					t.Fatal("confirmation must show actual target and SQL")
				}
				return tc.approve, nil
			})
			if called != tc.confirm || a.database.busy != tc.run {
				t.Fatalf("confirmation=%v running=%v", called, a.database.busy)
			}
			if tc.run {
				select {
				case ev := <-a.database.done:
					if ev.err != nil {
						t.Fatal(ev.err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("execution did not finish")
				}
			}
			if tc.confirm {
				r, e := dbRun(context.Background(), conn, "SELECT name FROM sqlite_master WHERE name='example'", nil, false)
				if e != nil {
					t.Fatal(e)
				}
				if (len(r.Rows) == 1) != tc.approve {
					t.Fatal("cancel/confirm did not control write")
				}
			}
		})
	}
}

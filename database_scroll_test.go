package main

import (
	"fmt"
	"github.com/HyperparameterSI/golet/backend/ui"
	"image/png"
	"os"
	"testing"
)

func TestDatabaseWideResultScroll(t *testing.T) {
	a := newApp("")
	a.openDatabasePanel()
	a.database.connections = []dbConnection{{ID: "wide", Name: "Wide results", Engine: "SQLite"}}
	a.database.active = "wide"
	for i := 0; i < 20; i++ {
		a.database.result.Columns = append(a.database.result.Columns, fmt.Sprintf("column_%02d", i))
	}
	for row := 0; row < 50; row++ {
		values := make([]any, 20)
		for col := range values {
			values[col] = fmt.Sprintf("value_%02d_%02d", row, col)
		}
		a.database.result.Rows = append(a.database.result.Rows, values)
	}
	a.database.status = "查询完成"
	v := ui.NewTester(a.view, 1000, 700)
	table, ok := v.Find("数据库查询结果")
	if !ok {
		t.Fatal("table missing")
	}
	status, _ := v.Find("查询完成")
	if table.Y+table.H > status.Y || table.X+table.W > 984 {
		t.Fatalf("table or scrollbar exceeds result viewport: table=%+v status=%+v", table, status)
	}
	v.Scroll(float32(table.X+table.W/2), float32(table.Y+60), 4000, 0)
	if os.Getenv("OMNICODE_DATABASE_SCREENSHOT") != "" {
		os.MkdirAll("artifacts", 0755)
		f, e := os.Create("artifacts/database-wide.png")
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		if e = png.Encode(f, v.Image()); e != nil {
			t.Fatal(e)
		}
	}
	last, ok := v.Find("column_19")
	if !ok || last.X < table.X || last.X >= table.X+table.W {
		t.Fatalf("last column not reachable: table=%+v last=%+v found=%v", table, last, ok)
	}
	cell, ok := v.Find("value_00_19")
	if !ok || cell.X < last.X || cell.X > last.X+30 {
		t.Fatalf("header/body out of sync: header=%+v cell=%+v", last, cell)
	}
	v.Scroll(float32(table.X+table.W/2), float32(table.Y+60), 0, 600)
	after, ok := v.Find("column_19")
	if !ok || after.Y != last.Y {
		t.Fatal("header moved vertically")
	}
	// Drag the persistent horizontal thumb back to the left edge.
	v.Press(table.X+table.W-70, table.Y+table.H-6)
	v.Move(table.X+10, table.Y+table.H-6)
	v.Release(table.X+10, table.Y+table.H-6)
	first, ok := v.Find("column_00")
	if !ok || first.W == 0 || first.X < table.X || first.X > table.X+30 {
		t.Fatalf("scrollbar drag did not reveal first column: %+v", first)
	}
	v.SetSize(800, 600)
	table, _ = v.Find("数据库查询结果")
	v.Scroll(table.X+table.W/2, table.Y+60, 4000, 0)
	last, ok = v.Find("column_19")
	if !ok || last.W == 0 || last.X >= 800 {
		t.Fatalf("resized table cannot reach last column: %+v", last)
	}
}

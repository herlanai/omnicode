package main

import (
	"archive/zip"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/xuri/excelize/v2"
)

func officeScreenshot(t *testing.T, name string, tester *ui.Tester) {
	t.Helper()
	if dir := os.Getenv("OMNICODE_VIEWER_ARTIFACTS"); dir != "" {
		os.MkdirAll(dir, 0755)
		f, e := os.Create(filepath.Join(dir, name))
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		if e = png.Encode(f, tester.Image()); e != nil {
			t.Fatal(e)
		}
	}
}
func TestSpreadsheetReadEditAndConflict(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Budget.xlsx")
	f := excelize.NewFile()
	f.SetCellValue("Sheet1", "A1", "项目")
	f.SetCellValue("Sheet1", "B1", "预算")
	f.SetCellValue("Sheet1", "A2", "原生编辑器")
	f.SetCellValue("Sheet1", "B2", 1200)
	f.SetCellFormula("Sheet1", "B3", "SUM(B2:B2)")
	f.NewSheet("Notes")
	f.SetCellValue("Notes", "A1", "保留这页")
	if e := f.SaveAs(path); e != nil {
		t.Fatal(e)
	}
	f.Close()
	d, e := loadDocument(path)
	if e != nil {
		t.Fatal(e)
	}
	if d.kind != spreadsheetFile || len(d.sheet.sheets) != 2 {
		t.Fatal("workbook routing failed")
	}
	d.sheet.selectCell(1, 1)
	if d.sheet.cell != "B2" || d.sheet.value != "1200" {
		t.Fatal("cell selection failed")
	}
	d.sheet.value = "2400"
	if e = d.applyCell(); e != nil {
		t.Fatal(e)
	}
	saved, e := excelize.OpenFile(path)
	if e != nil {
		t.Fatal(e)
	}
	v, _ := saved.GetCellValue("Sheet1", "B2")
	if v != "2400" {
		t.Fatal(v)
	}
	v, _ = saved.GetCellFormula("Sheet1", "B3")
	if v != "SUM(B2:B2)" {
		t.Fatal("formula lost")
	}
	v, _ = saved.GetCellValue("Notes", "A1")
	if v != "保留这页" {
		t.Fatal("other sheet lost")
	}
	saved.Close()
	a := newApp(root)
	a.open(path)
	tester := ui.NewTester(a.view, 1200, 780)
	officeScreenshot(t, "spreadsheet.png", tester)
	if e = tester.Click("预算"); e != nil {
		t.Fatal(e)
	}
	if a.current().sheet.cell != "B1" {
		t.Fatal("table cell click did not select address")
	}
	if e = tester.Click("单元格内容"); e != nil {
		t.Fatal(e)
	}
	tester.Key(ui.Cmd, ui.KeyA)
	tester.Type("年度预算")
	tester.Key(0, ui.KeyEnter)
	if a.current().sheet.err != "" || a.current().sheet.rows[0][1] != "年度预算" {
		t.Fatalf("cell edit UI failed: %s", a.current().sheet.err)
	}

	put(t, path, "external")
	d.sheet.value = "999"
	if e = d.applyCell(); !errors.Is(e, errConflict) {
		t.Fatalf("expected conflict, got %v", e)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "external" {
		t.Fatal("overwrote external change")
	}
	csvPath := filepath.Join(root, "data.csv")
	put(t, csvPath, "Name,Notes\nAlice,\"line one\nline two\"\n")
	csvDoc, e := loadDocument(csvPath)
	if e != nil {
		t.Fatal(e)
	}
	if len(csvDoc.sheet.rows) != 2 || csvDoc.sheet.rows[1][1] != "line one\nline two" {
		t.Fatal("CSV parsing failed")
	}
	if e = csvDoc.applyCell(); e == nil {
		t.Fatal("CSV should be read-only")
	}
}
func makePPTX(t *testing.T, path string) {
	t.Helper()
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	parts := map[string]string{
		"ppt/presentation.xml":             `<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><p:sldIdLst><p:sldId id="256" r:id="r2"/><p:sldId id="257" r:id="r1"/></p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels":  `<Relationships><Relationship Id="r1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/><Relationship Id="r2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide2.xml"/></Relationships>`,
		"ppt/slides/slide1.xml":            `<sld><p><r><t>第二页</t></r></p></sld>`,
		"ppt/slides/slide2.xml":            `<sld><p><r><t>项目计划</t></r></p><p><r><t>原生界面</t></r><br/><r><t>读取和整理项目资料</t></r></p></sld>`,
		"ppt/slides/_rels/slide2.xml.rels": `<Relationships><Relationship Id="n1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/notesSlide" Target="../notesSlides/notesSlide1.xml"/></Relationships>`,
		"ppt/notesSlides/notesSlide1.xml":  `<notes><p><r><t>演讲者备注</t></r></p></notes>`,
	}
	for name, body := range parts {
		part, e := w.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = part.Write([]byte(body)); e != nil {
			t.Fatal(e)
		}
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
}
func TestPresentationOrderingNotesAndUI(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Plan.pptx")
	makePPTX(t, path)
	d, e := loadDocument(path)
	if e != nil {
		t.Fatal(e)
	}
	if len(d.slides.slides) != 2 || !strings.HasPrefix(d.slides.slides[0].Text, "项目计划") || d.slides.slides[0].Notes != "演讲者备注" {
		t.Fatal("slide ordering or notes wrong")
	}
	a := newApp(root)
	a.open(path)
	tester := ui.NewTester(a.view, 1200, 780)
	if e = tester.Click("文字与备注"); e != nil {
		t.Fatal(e)
	}
	officeScreenshot(t, "presentation.png", tester)
	if e = tester.Click("下一页"); e != nil {
		t.Fatal(e)
	}
	if a.current().slides.page != 1 {
		t.Fatal("navigation failed")
	}
	put(t, path, "bad zip")
	if _, e = loadDocument(path); e == nil {
		t.Fatal("accepted corrupt PPTX")
	}
}

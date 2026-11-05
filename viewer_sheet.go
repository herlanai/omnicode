package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/forma"
	"github.com/xuri/excelize/v2"
)

const sheetRowLimit = 10000
const sheetColumnLimit = 100

type spreadsheetView struct {
	data                     []byte
	sheets                   []string
	selected                 string
	rows                     [][]string
	columns                  int
	list                     ui.ListState
	cell, value, filter, err string
	matches                  []int
	truncated                bool
	csv                      bool
	readOnly                 bool
}

func readOfficeFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return nil, errors.New("数据文件仅支持 64 MiB 以内的普通文件")
	}
	b, err := io.ReadAll(io.LimitReader(f, (64<<20)+1))
	if len(b) > 64<<20 {
		return nil, errors.New("文件过大")
	}
	return b, err
}
func openWorkbook(data []byte) (*excelize.File, error) {
	return excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: 256 << 20, UnzipXMLSizeLimit: 16 << 20})
}
func loadSpreadsheet(path string) (*document, error) {
	data, err := readOfficeFile(path)
	if err != nil {
		return nil, err
	}
	s := &spreadsheetView{data: data, cell: "A1"}
	ext := strings.ToLower(filepath.Ext(path))
	s.csv = ext == ".csv" || ext == ".tsv"
	s.readOnly = ext != ".xlsx"
	if s.csv {
		s.sheets = []string{filepath.Base(path)}
	} else {
		f, e := openWorkbook(data)
		if e != nil {
			return nil, e
		}
		s.sheets = f.GetSheetList()
		f.Close()
	}
	if len(s.sheets) == 0 {
		return nil, errors.New("没有可读取的工作表")
	}
	s.selected = s.sheets[0]
	if err = s.loadRows(); err != nil {
		return nil, err
	}
	d := newDocument(filepath.Base(path), "")
	d.path = path
	d.kind = spreadsheetFile
	d.sheet = s
	d.diskHash = sha256.Sum256(data)
	return d, nil
}
func (s *spreadsheetView) loadRows() error {
	var rows [][]string
	truncated := false
	if s.csv {
		if !utf8.Valid(s.data) {
			return errors.New("CSV/TSV 需要 UTF-8 编码")
		}
		r := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(s.data), "\ufeff")))
		r.FieldsPerRecord = -1
		if strings.HasSuffix(strings.ToLower(s.selected), ".tsv") {
			r.Comma = '\t'
		}
		for {
			row, err := r.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if len(rows) == sheetRowLimit {
				truncated = true
				break
			}
			if len(row) > sheetColumnLimit {
				row = row[:sheetColumnLimit]
				truncated = true
			}
			rows = append(rows, row)
		}
	} else {
		f, err := openWorkbook(s.data)
		if err != nil {
			return err
		}
		defer f.Close()
		it, err := f.Rows(s.selected)
		if err != nil {
			return err
		}
		defer it.Close()
		for it.Next() {
			if len(rows) == sheetRowLimit {
				truncated = true
				break
			}
			row, err := it.Columns()
			if err != nil {
				return err
			}
			if len(row) > sheetColumnLimit {
				row = row[:sheetColumnLimit]
				truncated = true
			}
			rows = append(rows, row)
		}
		if err = it.Error(); err != nil {
			return err
		}
	}
	s.rows = rows
	s.truncated = truncated
	s.columns = 1
	for _, row := range rows {
		s.columns = max(s.columns, len(row))
	}
	s.filter = ""
	s.matches = nil
	s.list = ui.ListState{}
	s.cell = "A1"
	s.value = ""
	s.err = ""
	return nil
}
func (s *spreadsheetView) selectCell(row, col int) {
	s.cell, _ = excelize.CoordinatesToCellName(col+1, row+1)
	s.value = ""
	if row < len(s.rows) && col < len(s.rows[row]) {
		s.value = s.rows[row][col]
	}
	if !s.csv {
		f, err := openWorkbook(s.data)
		if err == nil {
			defer f.Close()
			if formula, e := f.GetCellFormula(s.selected, s.cell); e == nil && formula != "" {
				s.value = "=" + formula
			} else if raw, e := f.GetCellValue(s.selected, s.cell, excelize.Options{RawCellValue: true}); e == nil {
				s.value = raw
			}
		}
	}
}

// Apply one explicit cell edit to a fresh workbook, leaving the original untouched on failure.
func (d *document) applyCell() error {
	s := d.sheet
	if s.readOnly {
		return errors.New("此格式为只读数据视图")
	}
	col, row, err := excelize.CellNameToCoordinates(s.cell)
	if err != nil || col < 1 || row < 1 {
		return errors.New("单元格地址无效")
	}
	f, err := openWorkbook(s.data)
	if err != nil {
		return err
	}
	defer f.Close()
	value := s.value
	if strings.HasPrefix(value, "=") {
		err = f.SetCellFormula(s.selected, s.cell, strings.TrimPrefix(value, "="))
	} else {
		if err = f.SetCellFormula(s.selected, s.cell, ""); err != nil {
			return err
		}
		if strings.HasPrefix(value, "'") {
			err = f.SetCellStr(s.selected, s.cell, value[1:])
		} else if n, e := strconv.ParseFloat(value, 64); e == nil {
			err = f.SetCellValue(s.selected, s.cell, n)
		} else {
			err = f.SetCellStr(s.selected, s.cell, value)
		}
	}
	if err != nil {
		return err
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		return err
	}
	if err = d.replaceOfficeBytes(buf.Bytes()); err != nil {
		return err
	}
	cell := s.cell
	s.data = buf.Bytes()
	if err = s.loadRows(); err != nil {
		return err
	}
	s.cell = cell
	s.value = value
	return nil
}
func (d *document) replaceOfficeBytes(data []byte) error {
	disk, err := os.ReadFile(d.path)
	if err != nil {
		return err
	}
	if sha256.Sum256(disk) != d.diskHash {
		return errConflict
	}
	info, err := os.Stat(d.path)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(d.path), ".omnicode-office-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(info.Mode().Perm()); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	disk, err = os.ReadFile(d.path)
	if err != nil {
		return err
	}
	if sha256.Sum256(disk) != d.diskHash {
		return errConflict
	}
	if err = os.Rename(f.Name(), d.path); err != nil {
		return err
	}
	d.diskHash = sha256.Sum256(data)
	return nil
}
func (a *app) spreadsheetContent(c *ui.Context, d *document) {
	s := d.sheet
	ui.Row(c).FillWidth().Height(42).Padding(0, 12).Gap(8).AlignItems(ui.Center).Children(func() {
		if forma.Select(c.Key("worksheet"), &s.selected, s.sheets).Width(150).Label("工作表").Changed() {
			s.err = ""
			if err := s.loadRows(); err != nil {
				s.err = err.Error()
			}
		}
		muted(c, fmt.Sprintf("%d 行 · %d 列", len(s.rows), s.columns))
		ui.Box(c).Grow(1)
		forma.Input(c.Key("sheet-filter"), &s.filter, forma.InputOptions{Placeholder: "筛选行…"}).Width(180).Label("筛选行")
		iconButton(c, "copy-sheet", "files", "复制当前表格", func() {
			var b strings.Builder
			for _, row := range s.rows {
				b.WriteString(strings.Join(row, "\t"))
				b.WriteByte('\n')
			}
			c.WriteClipboard(b.String())
		})
	})
	ui.Row(c).FillWidth().Height(40).Padding(0, 12).Gap(8).AlignItems(ui.Center).Children(func() {
		forma.Input(c.Key("cell-address"), &s.cell, forma.InputOptions{ReadOnly: s.readOnly}).Width(70).Label("单元格地址")
		input := forma.Input(c.Key("cell-value"), &s.value, forma.InputOptions{Placeholder: "选择单元格；回车应用修改", ReadOnly: s.readOnly}).Grow(1).Label("单元格内容")
		if !s.readOnly {
			apply := input.Submitted()
			if iconButton(c, "apply-cell", "check", "应用并保存单元格（Enter）", func() { apply = true }).Clicked() {
				apply = true
			}
			if apply {
				if err := d.applyCell(); err != nil {
					s.err = err.Error()
				} else {
					s.err = ""
					a.notice = "单元格已保存"
				}
			}
		}
	})
	if s.err != "" {
		ui.Text(c, s.err).TextColor(c.Theme().Danger)
	}
	if s.truncated {
		muted(c, "仅展示前 10,000 行 / 100 列；保存保留其余数据")
	}
	columns := []ui.TableColumn{{Title: "#", Width: 50, Fixed: true, RowHeader: true}}
	for i := 0; i < s.columns; i++ {
		name, _ := excelize.ColumnNumberToName(i + 1)
		columns = append(columns, ui.TableColumn{Title: name, Width: 140, MinWidth: 70})
	}
	s.matches = s.matches[:0]
	for i, row := range s.rows {
		if s.filter == "" || strings.Contains(strings.ToLower(strings.Join(row, "\t")), strings.ToLower(s.filter)) {
			s.matches = append(s.matches, i)
		}
	}
	ui.Table(c.Key("sheet-table"), &s.list, columns, len(s.matches), func(row, col int) {
		actual := s.matches[row]
		if col == 0 {
			muted(c, strconv.Itoa(actual+1))
			return
		}
		value := ""
		if col-1 < len(s.rows[actual]) {
			value = s.rows[actual][col-1]
		}
		ui.Text(c, value).FillWidth().MaxLines(1).Tooltip(value).OnClick(func() { s.selectCell(actual, col-1) })
	}).Grow(1).FillWidth()
}

package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/HyperparameterSI/golet/backend/ui/editor"
)

const maxFileSize = 8 << 20

var errConflict = errors.New("文件已被其他程序修改，请重新打开或另存为")

type document struct {
	vim         vimEditor
	http        *httpPanel
	sheet       *spreadsheetView
	slides      *slidesView
	kind        fileKind
	media       *mediaView
	path, name  string
	state       *editor.State
	diskHash    [32]byte
	preview     bool
	autoVersion uint64
	autoDue     time.Time
	autoFailed  bool
	saveError   string
}

func newDocument(name, text string) *document {
	b := editor.NewBuffer(text)
	b.SetLanguage(editor.LanguageFor(name))
	kind := textFile
	if kindFor(name) == markdownFile {
		kind = markdownFile
	}
	return &document{kind: kind, name: name, state: &editor.State{Buffer: b, LineHeight: 1.65, Label: name}}
}
func readText(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("只能打开普通文本文件")
	}
	if info.Size() > maxFileSize {
		return nil, errors.New("文件超过 8 MiB，暂不支持打开")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileSize || bytes.ContainsRune(data, 0) || !utf8.Valid(data) {
		return nil, errors.New("不支持的文件：请使用 UTF-8 文本")
	}
	return data, nil
}
func loadDocument(path string) (*document, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".xls", ".xlsb", ".ppt":
		return nil, errors.New("暂不支持旧版 Office 格式，请转换为 XLSX 或 PPTX 后打开")
	}
	switch kindFor(path) {
	case spreadsheetFile:
		return loadSpreadsheet(path)
	case presentationFile:
		return loadSlides(path)
	}
	if kind := kindFor(path); kind == imageFile || kind == pdfFile {
		return loadMedia(path, kind)
	}
	data, err := readText(path)
	if err != nil {
		return nil, err
	}
	d := newDocument(filepath.Base(path), string(data))
	d.path = path
	d.diskHash = sha256.Sum256(data)
	return d, nil
}

// Save beside the destination, then atomically replace it. Refuse external changes.
func (d *document) save(path string) error {
	if !d.editable() {
		return errors.New("图片和 PDF 为只读视图")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if resolved, e := filepath.EvalSymlinks(path); e == nil {
		path = resolved
	} else if !os.IsNotExist(e) {
		return e
	}
	mode := os.FileMode(0644)
	if info, e := os.Stat(path); e == nil {
		if !info.Mode().IsRegular() {
			return errors.New("目标不是普通文件")
		}
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(e) {
		return e
	}
	if path == d.path {
		data, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		if sha256.Sum256(data) != d.diskHash {
			return errConflict
		}
	}
	text := d.state.Buffer.String()
	if d.state.Buffer.LineEnding() == "\r\n" {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".omnicode-save-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if err = f.Chmod(mode); err == nil {
		_, err = f.WriteString(text)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(temp, path); err != nil {
		return err
	}
	d.path = path
	d.name = filepath.Base(path)
	d.diskHash = sha256.Sum256([]byte(text))
	d.state.Buffer.MarkSaved()
	d.autoDue = time.Time{}
	d.autoFailed = false
	d.saveError = ""
	d.state.Buffer.SetLanguage(editor.LanguageFor(path))
	d.state.Label = d.name
	d.kind = textFile
	if kindFor(d.name) == markdownFile {
		d.kind = markdownFile
	}
	return nil
}

type entry struct {
	name, path string
	dir        bool
	children   []*entry
	loaded     bool
}

func children(path string) ([]*entry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]*entry, 0, len(entries))
	for _, e := range entries {
		if e.Name() == ".git" || e.Name() == ".DS_Store" {
			continue
		}
		out = append(out, &entry{name: e.Name(), path: filepath.Join(path, e.Name()), dir: e.IsDir()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].dir != out[j].dir {
			return out[i].dir
		}
		return strings.ToLower(out[i].name) < strings.ToLower(out[j].name)
	})
	return out, nil
}
func fileIndex(root string) []string {
	var paths []string
	_ = filepath.WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if len(paths) >= 20000 {
			return filepath.SkipAll
		}
		if e.IsDir() {
			switch e.Name() {
			case ".git", "node_modules", "vendor", ".idea", "bin", "artifacts":
				return filepath.SkipDir
			}
			return nil
		}
		if e.Type().IsRegular() {
			rel, _ := filepath.Rel(root, path)
			paths = append(paths, rel)
		}
		return nil
	})
	return paths
}
func (d *document) markdown() bool { return d.editable() && kindFor(d.name) == markdownFile }
func (d *document) description() string {
	if d.path != "" {
		return d.path
	}
	return fmt.Sprintf("%s · 尚未保存", d.name)
}

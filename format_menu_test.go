package main

import (
	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/editor"
	"go/format"
	"testing"
)

func TestFormatDocumentContextMenuUndo(t *testing.T) {
	a := newApp("")
	source := "package main\nfunc main(){println(1)}\n"
	d := newDocument("main.go", source)
	v := ui.NewTester(func(c *ui.Context) { a.textContent(c, d) }, 800, 500)
	v.RightClickAt(150, 35)
	if err := v.ChooseMenuItem("Format Document"); err != nil {
		t.Fatal(err)
	}
	want, _ := format.Source([]byte(source))
	if d.state.Buffer.String() != string(want) {
		t.Fatal("document not formatted")
	}
	d.state.Run(editor.Undo)
	if d.state.Buffer.String() != source {
		t.Fatal("format cannot be undone")
	}
	a.git.mutating = true
	a.formatDocument(d)
	if d.state.Buffer.String() != source {
		t.Fatal("format changed buffer during git operation")
	}
}

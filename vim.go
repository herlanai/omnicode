package main

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/editor"
)

type vimRegister struct {
	text     string
	linewise bool
}

type vimEditor struct {
	mode                 string
	pending              rune
	count, operatorCount int
	anchor, head         int
}

func (v *vimEditor) label() string {
	if v.mode == "" {
		return "NORMAL"
	}
	return v.mode
}

func (v *vimEditor) configure(st *editor.State, enabled bool, reg *vimRegister) {
	if !enabled {
		*v = vimEditor{}
		st.OnInput, st.NoInputMethod, st.CursorShape = nil, false, editor.CursorBar
		return
	}
	st.NoInputMethod = v.mode != "INSERT"
	st.CursorShape = editor.CursorBlock
	if !st.NoInputMethod {
		st.CursorShape = editor.CursorBar
	}
	st.OnInput = func(ev ui.InputEvent) bool {
		if ev.Kind == ui.InputPointerDown && v.mode != "INSERT" {
			*v = vimEditor{}
		}
		if ev.Kind == ui.InputKeyDown {
			if ev.Key == ui.KeyEscape {
				pos := st.Cursor()
				if v.mode == "INSERT" && st.CursorPos().Col > 0 {
					pos = vimPrev(st.Buffer, pos)
				}
				if v.mode == "VISUAL" {
					pos = v.head
				}
				*v = vimEditor{}
				st.Run(editor.Cancel)
				st.Select(pos, pos)
				return true
			}
			if v.mode != "INSERT" {
				if ev.Mods == ui.Ctrl && ev.Key == ui.KeyR {
					if !st.ReadOnly {
						st.Run(editor.Redo)
					}
					return true
				}
				if ev.Mods == 0 {
					switch ev.Key {
					case ui.KeyLeft, ui.KeyRight, ui.KeyUp, ui.KeyDown:
						v.command(st, map[ui.Key]rune{ui.KeyLeft: 'h', ui.KeyRight: 'l', ui.KeyUp: 'k', ui.KeyDown: 'j'}[ev.Key], reg)
						return true
					case ui.KeyEnter, ui.KeyTab, ui.KeyBackspace, ui.KeyDelete:
						return true
					}
				}
			}
		}
		if ev.Kind != ui.InputText || v.mode == "INSERT" {
			return false
		}
		for _, key := range ev.Text {
			v.command(st, key, reg)
		}
		return true
	}
}

func vimNext(b *editor.Buffer, pos int) int {
	_, n := utf8.DecodeRuneInString(b.Slice(pos, min(b.Len(), pos+utf8.UTFMax)))
	return pos + n
}
func vimPrev(b *editor.Buffer, pos int) int {
	_, n := utf8.DecodeLastRuneInString(b.Slice(max(0, pos-utf8.UTFMax), pos))
	return pos - n
}

func vimClass(b *editor.Buffer, pos int) int {
	r, _ := utf8.DecodeRuneInString(b.Slice(pos, min(b.Len(), pos+utf8.UTFMax)))
	switch {
	case unicode.IsSpace(r):
		return 0
	case unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_':
		return 1
	default:
		return 2
	}
}

func vimMotion(st *editor.State, pos int, key rune) int {
	b := st.Buffer
	s := b.Snapshot()
	line := s.LineOf(pos)
	start, end := s.LineStart(line), s.LineEnd(line)
	switch key {
	case 'h':
		return max(start, vimPrev(b, pos))
	case 'l':
		if pos < end {
			return min(max(start, vimPrev(b, end)), vimNext(b, pos))
		}
	case '0':
		return start
	case '$':
		return max(start, vimPrev(b, end))
	case 'j', 'k':
		next := line + 1
		if key == 'k' {
			next = line - 1
		}
		if next < 0 || next >= s.LineCount() {
			return pos
		}
		col := utf8.RuneCountInString(s.Slice(start, pos))
		i := s.LineStart(next)
		for range col {
			n := vimNext(b, i)
			if n >= s.LineEnd(next) {
				break
			}
			i = n
		}
		return i
	case 'w':
		class := vimClass(b, pos)
		for pos < s.Len() && vimClass(b, pos) == class {
			pos = vimNext(b, pos)
		}
		for pos < s.Len() && vimClass(b, pos) == 0 {
			pos = vimNext(b, pos)
		}
	case 'b':
		pos = vimPrev(b, pos)
		for pos > 0 && vimClass(b, pos) == 0 {
			pos = vimPrev(b, pos)
		}
		class := vimClass(b, pos)
		for pos > 0 && vimClass(b, vimPrev(b, pos)) == class {
			pos = vimPrev(b, pos)
		}
	}
	return pos
}

func (v *vimEditor) command(st *editor.State, key rune, reg *vimRegister) {
	b := st.Buffer
	pos := st.Cursor()
	visual := v.mode == "VISUAL"
	if visual {
		pos = min(v.head, b.Len())
	}
	if key >= '0' && key <= '9' && (key != '0' || v.count > 0) {
		v.count = min(999, v.count*10+int(key-'0'))
		return
	}
	explicitCount := v.count != 0 || v.operatorCount != 0
	n := min(999, max(1, v.count)*max(1, v.operatorCount))
	s := b.Snapshot()
	line := s.LineOf(pos)
	start, end := s.LineStart(line), s.LineEnd(line)
	op := v.pending
	if !visual && op == 0 && strings.ContainsRune("dycg", key) {
		v.pending = key
		v.operatorCount, v.count = v.count, 0
		return
	}
	v.pending, v.count, v.operatorCount = 0, 0, 0
	dest := pos
	linewise := false
	switch {
	case op == 'g' && key == 'g' || op == 0 && key == 'G':
		target := min(n, s.LineCount()) - 1
		if key == 'G' && !explicitCount {
			target = s.LineCount() - 1
		}
		dest = s.LineStart(target)
	case op == 'g':
		return
	case op != 0 && (op == key || key == 'j' || key == 'k'):
		if key == 'j' {
			n++
		}
		if key == 'k' {
			first := max(0, line-n)
			n = line - first + 1
			line = first
			start = s.LineStart(first)
		}
		linewise = true
		pos = start
		dest = s.LineStart(min(line+n, s.LineCount()-1))
		if line+n >= s.LineCount() {
			dest = s.Len()
		}
	case strings.ContainsRune("hjklwb0$", key):
		for range n {
			dest = vimMotion(st, dest, key)
		}
		if op != 0 && key == 'l' && dest < end && vimNext(b, dest) == end && utf8.RuneCountInString(b.Slice(pos, end)) <= n {
			dest = end
		}
		if op != 0 && key == '$' {
			dest = end
		}
	case visual && strings.ContainsRune("dycx", key):
		op = key
		pos, dest = st.Selection().Start(), st.Selection().End()
	case op != 0:
		return
	case visual:
		if key == 'v' {
			v.mode = ""
			st.Select(pos, pos)
		}
		return
	case key == 'v':
		v.mode = "VISUAL"
		v.anchor = pos
		v.head = pos
		st.Select(pos, vimNext(b, pos))
		return
	default:
		if st.ReadOnly {
			return
		}
		switch key {
		case 'i', 'a', 'A', 'I', 'o', 'O':
			switch key {
			case 'a':
				pos = min(end, vimNext(b, pos))
			case 'A':
				pos = end
			case 'I':
				pos = start + len(s.Line(line)) - len(strings.TrimLeft(s.Line(line), " \t"))
			case 'o', 'O':
				cmd := editor.NewlineBelow
				if key == 'O' {
					cmd = editor.NewlineAbove
				}
				st.Run(cmd)
				pos = st.Cursor()
			}
			v.mode = "INSERT"
			st.Select(pos, pos)
		case 'u':
			for range n {
				st.Run(editor.Undo)
			}
		case '/':
			st.Run(editor.Find)
		case 'n':
			st.Run(editor.FindNext)
		case 'N':
			st.Run(editor.FindPrevious)
		case 'x':
			op = 'd'
			for range n {
				dest = min(end, vimNext(b, dest))
			}
		case 'p', 'P':
			if reg.text == "" {
				return
			}
			text := reg.text
			if reg.linewise {
				pos = start
				if key == 'p' {
					pos = end
					if line+1 < s.LineCount() {
						pos++
					} else {
						text = "\n" + strings.TrimSuffix(text, "\n")
					}
				}
			} else if key == 'p' {
				pos = min(end, vimNext(b, pos))
			}
			st.Select(pos, pos)
			st.Insert(text)
			if reg.linewise && key == 'p' && line+1 == s.LineCount() {
				pos++
			}
			if !reg.linewise {
				pos = vimPrev(b, st.Cursor())
			}
			st.Select(pos, pos)
		}
		if op == 0 {
			return
		}
	}
	if op != 0 && op != 'g' {
		if st.ReadOnly && op != 'y' {
			return
		}
		lo, hi := min(pos, dest), max(pos, dest)
		reg.text, reg.linewise = b.Slice(lo, hi), linewise
		if linewise && !strings.HasSuffix(reg.text, "\n") {
			reg.text += "\n"
		}
		if op != 'y' && (hi > lo || linewise) {
			if linewise && hi == s.Len() && lo > 0 && op != 'c' && !strings.HasSuffix(s.Slice(lo, hi), "\n") {
				lo--
			}
			st.Select(lo, hi)
			if linewise && op == 'c' && hi < s.Len() {
				st.Insert("\n")
			} else if hi > lo {
				st.Run(editor.Delete)
			}
		}
		v.mode = ""
		if op == 'c' {
			v.mode = "INSERT"
		}
		st.Select(lo, lo)
		return
	}
	if visual {
		v.head = dest
		if dest < v.anchor {
			st.Select(vimNext(b, v.anchor), dest)
		} else {
			st.Select(v.anchor, vimNext(b, dest))
		}
	} else {
		st.Select(dest, dest)
	}
}

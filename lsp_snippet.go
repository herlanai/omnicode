package main

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Expand standard numbered LSP placeholders into editable text. Unsupported
// transforms/variables are rejected instead of leaking snippet syntax to code.
func plainSnippet(s string) (text string, caret int, ok bool) {
	var b strings.Builder
	best := int(^uint(0) >> 1)
	caret = -1
	for i := 0; i < len(s); {
		if s[i] == '\\' && i+1 < len(s) {
			b.WriteByte(s[i+1])
			i += 2
			continue
		}
		if s[i] != '$' {
			b.WriteByte(s[i])
			i++
			continue
		}
		i++
		braced := i < len(s) && s[i] == '{'
		if braced {
			i++
		}
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if start == i {
			return "", 0, false
		}
		n, _ := strconv.Atoi(s[start:i])
		rank := n
		if n == 0 {
			rank = int(^uint(0)>>1) - 1
		}
		if rank < best {
			best = rank
			caret = b.Len()
		}
		if !braced {
			continue
		}
		if i >= len(s) {
			return "", 0, false
		}
		switch s[i] {
		case '}':
			i++
		case ':':
			i++
			start = i
			depth := 0
			for i < len(s) {
				if s[i] == '\\' && i+1 < len(s) {
					i += 2
					continue
				}
				if s[i] == '{' {
					depth++
				}
				if s[i] == '}' {
					if depth == 0 {
						break
					}
					depth--
				}
				i++
			}
			if i == len(s) {
				return "", 0, false
			}
			v, _, valid := plainSnippet(s[start:i])
			if !valid {
				return "", 0, false
			}
			b.WriteString(v)
			i++
		case '|':
			i++
			start = i
			for i < len(s) && s[i] != ',' && s[i] != '|' {
				i++
			}
			b.WriteString(s[start:i])
			for i+1 < len(s) && s[i:i+2] != "|}" {
				i++
			}
			if i+1 >= len(s) {
				return "", 0, false
			}
			i += 2
		default:
			return "", 0, false
		}
	}
	if caret < 0 {
		caret = b.Len()
	}
	return b.String(), caret, true
}

// Golet owns the actual insertion/undo. Only move the caret after an exact,
// single completion edit, never after arbitrary typing or a server response.
type completionInsertion struct {
	job    languageJob
	carets map[string]int
}

func lspSnippetCarets(raw json.RawMessage) map[string]int {
	var items []lspCompletionItem
	if json.Unmarshal(raw, &items) != nil {
		var list struct{ Items []lspCompletionItem }
		_ = json.Unmarshal(raw, &list)
		items = list.Items
	}
	carets := map[string]int{}
	for _, it := range items {
		if it.InsertTextFormat != 2 {
			continue
		}
		text := it.InsertText
		if it.TextEdit != nil {
			text = it.TextEdit.NewText
		}
		if plain, caret, ok := plainSnippet(text); ok {
			carets[plain] = caret
		}
	}
	return carets
}
func (a *app) positionAcceptedCompletion() {
	for d, p := range a.intelligence.accepted {
		if d.state.Buffer.Version() == p.job.target.version {
			continue
		}
		delete(a.intelligence.accepted, d)
		if a.current() != d || d.state.Buffer.Version() != p.job.target.version+1 || len(d.state.Selections()) != 1 {
			continue
		}
		before := p.job.target.text
		after := d.state.Buffer.String()
		start, end := p.job.start, p.job.offset
		if !strings.HasPrefix(after, before[:start]) || !strings.HasSuffix(after, before[end:]) {
			continue
		}
		insertedEnd := len(after) - (len(before) - end)
		if insertedEnd < start || d.state.Cursor() != insertedEnd {
			continue
		}
		if caret, ok := p.carets[after[start:insertedEnd]]; ok {
			d.state.Select(start+caret, start+caret)
		}
	}
}

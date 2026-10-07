package main

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var (
	colAccent = tcell.NewHexColor(0xf0883e)
	colBar    = tcell.NewHexColor(0x262c36)
	toneColor = map[tone]tcell.Color{
		toneNone:   tcell.ColorDefault,
		toneDim:    tcell.NewHexColor(0x6e7681),
		toneGood:   tcell.NewHexColor(0x3fb950),
		toneWarn:   tcell.NewHexColor(0xd29922),
		toneBad:    tcell.NewHexColor(0xf85149),
		toneAccent: colAccent,
	}
)

type row struct {
	key   string
	cells []cell
}

// list is a table that keeps its selection across refreshes and
// supports a simple substring filter.
type list struct {
	*tview.Table
	headers []string
	right   map[int]bool // right aligned columns
	expand  int
	rows    []row
	filter  string
	selKey  string
}

func newList(expand int, headers ...string) *list {
	l := &list{
		Table:   tview.NewTable(),
		headers: headers,
		right:   map[int]bool{},
		expand:  expand,
	}
	l.SetFixed(1, 0).SetSelectable(true, false)
	l.SetSelectedStyle(tcell.StyleDefault.Background(colAccent).Foreground(tcell.ColorBlack))
	l.SetSelectionChangedFunc(func(r, _ int) {
		if r > 0 {
			if ref, ok := l.GetCell(r, 0).GetReference().(string); ok {
				l.selKey = ref
			}
		}
	})
	return l
}

func (l *list) alignRight(cols ...int) *list {
	for _, c := range cols {
		l.right[c] = true
	}
	return l
}

func (l *list) setRows(rows []row) {
	l.rows = rows
	l.render()
}

func (l *list) setFilter(f string) {
	l.filter = strings.ToLower(f)
	l.render()
}

func (l *list) selected() string { return l.selKey }

func (l *list) render() {
	l.Clear()
	for i, h := range l.headers {
		c := tview.NewTableCell(" " + h).
			SetTextColor(colAccent).
			SetBackgroundColor(colBar).
			SetAttributes(tcell.AttrBold).
			SetSelectable(false)
		if l.right[i] {
			c.SetAlign(tview.AlignRight)
		}
		if i == l.expand {
			c.SetExpansion(1)
		}
		l.SetCell(0, i, c)
	}

	prev, _ := l.GetSelection()
	r, sel := 1, 0
	for _, rw := range l.rows {
		if l.filter != "" && !strings.Contains(strings.ToLower(rowText(rw)), l.filter) {
			continue
		}
		for i, c := range rw.cells {
			tc := tview.NewTableCell(" " + tview.Escape(c.text)).
				SetReference(rw.key).
				SetTextColor(toneColor[c.tone])
			if l.right[i] {
				tc.SetAlign(tview.AlignRight)
			}
			if i == l.expand {
				tc.SetExpansion(1)
			}
			l.SetCell(r, i, tc)
		}
		if rw.key == l.selKey {
			sel = r
		}
		r++
	}
	if r == 1 {
		return
	}
	if sel == 0 {
		// selected row went away, stay roughly where we were
		sel = min(max(prev, 1), r-1)
	}
	l.Select(sel, 0)
	if ref, ok := l.GetCell(sel, 0).GetReference().(string); ok {
		l.selKey = ref
	}
}

func rowText(r row) string {
	var b strings.Builder
	for _, c := range r.cells {
		b.WriteString(c.text)
		b.WriteByte(' ')
	}
	return b.String()
}

package main

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type row struct {
	key   string
	cells []string
	color tcell.Color
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
	empty   string
}

func newList(expand int, headers ...string) *list {
	l := &list{
		Table:   tview.NewTable(),
		headers: headers,
		right:   map[int]bool{},
		expand:  expand,
	}
	l.SetFixed(1, 0).SetSelectable(true, false)
	l.SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorTeal).Foreground(tcell.ColorBlack))
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
	l.empty = ""
	l.render()
}

// setEmpty replaces the table body with a single message, e.g. when
// docker is not running.
func (l *list) setEmpty(msg string) {
	l.rows = nil
	l.empty = msg
	l.render()
}

func (l *list) setFilter(f string) {
	l.filter = strings.ToLower(f)
	l.render()
}

func (l *list) selected() string { return l.selKey }

func (l *list) selectKey(k string) {
	l.selKey = k
	l.render()
}

func (l *list) render() {
	l.Clear()
	for i, h := range l.headers {
		c := tview.NewTableCell(h).
			SetTextColor(tcell.ColorBlack).
			SetBackgroundColor(tcell.ColorDarkCyan).
			SetSelectable(false)
		if l.right[i] {
			c.SetAlign(tview.AlignRight)
		}
		if i == l.expand {
			c.SetExpansion(1)
		}
		l.SetCell(0, i, c)
	}
	if l.empty != "" {
		l.SetCell(1, 0, tview.NewTableCell(l.empty).SetTextColor(tcell.ColorGray).SetSelectable(false))
		return
	}

	prev, _ := l.GetSelection()
	r, sel := 1, 0
	for _, rw := range l.rows {
		if l.filter != "" && !strings.Contains(strings.ToLower(strings.Join(rw.cells, " ")), l.filter) {
			continue
		}
		for i, text := range rw.cells {
			c := tview.NewTableCell(tview.Escape(text)).SetReference(rw.key)
			if rw.color != 0 {
				c.SetTextColor(rw.color)
			}
			if l.right[i] {
				c.SetAlign(tview.AlignRight)
			}
			if i == l.expand {
				c.SetExpansion(1)
			}
			l.SetCell(r, i, c)
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

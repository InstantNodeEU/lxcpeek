package main

import (
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/instantnodeeu/lxcpeek/internal/pve"
	"github.com/rivo/tview"
)

type app struct {
	*tview.Application
	header *tview.TextView
	list   *list
	footer *tview.Pages
	hints  *tview.TextView
	input  *tview.InputField
	root   *tview.Pages

	col      collector
	last     []guestStat
	sort     sortKey
	reverse  bool
	modal    bool
	interval time.Duration
	wake     chan struct{}
}

var sortKeys = map[rune]sortKey{'c': byCPU, 'm': byMem, 'n': byNet, 'k': byConns, 'o': byIO, 'i': byID}

const hintText = "c m n k o i sort  / filter  Enter connections  q quit"

func newApp(interval time.Duration) *app {
	a := &app{
		Application: tview.NewApplication(),
		header:      tview.NewTextView().SetDynamicColors(true).SetWrap(false),
		list:        newList(2, headers...).alignRight(0, 4, 5, 6, 7, 8, 9, 10),
		footer:      tview.NewPages(),
		hints:       tview.NewTextView().SetDynamicColors(true).SetWrap(false),
		input:       tview.NewInputField().SetLabel("/"),
		root:        tview.NewPages(),
		interval:    interval,
		wake:        make(chan struct{}, 1),
	}
	a.hints.SetText("[gray]" + hintText + "[-]")
	a.input.SetFieldBackgroundColor(tcell.ColorDefault)
	a.input.SetChangedFunc(a.list.setFilter)
	a.input.SetDoneFunc(func(k tcell.Key) {
		if k == tcell.KeyEscape {
			a.input.SetText("")
		}
		a.footer.SwitchToPage("hints")
		a.SetFocus(a.list)
	})
	a.footer.AddPage("hints", a.hints, true, true)
	a.footer.AddPage("input", a.input, true, false)

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.header, 2, 0, false).
		AddItem(a.list, 0, 1, true).
		AddItem(a.footer, 1, 0, false)
	a.root.AddPage("main", layout, true, true)
	a.list.SetSelectedFunc(func(int, int) { a.showConns() })
	a.SetRoot(a.root, true).SetInputCapture(a.keys)
	go a.loop()
	return a
}

func (a *app) keys(ev *tcell.EventKey) *tcell.EventKey {
	if a.modal || a.input.HasFocus() {
		return ev
	}
	if ev.Key() == tcell.KeyEscape {
		a.input.SetText("")
		return nil
	}
	if k, ok := sortKeys[ev.Rune()]; ok {
		if a.sort == k {
			a.reverse = !a.reverse
		} else {
			a.sort, a.reverse = k, false
		}
		a.render()
		return nil
	}
	switch r := ev.Rune(); {
	case r == 'q':
		a.Stop()
		return nil
	case r == '/':
		a.footer.SwitchToPage("input")
		a.SetFocus(a.input)
		return nil
	}
	return ev
}

func (a *app) loop() {
	t := time.NewTicker(a.interval)
	defer t.Stop()
	for {
		snap, err := a.col.collect()
		a.QueueUpdateDraw(func() {
			if err != nil {
				a.header.SetText("[red]" + tview.Escape(err.Error()) + "[-]")
				return
			}
			a.drawHeader(snap)
			a.last = snap.Guests
			a.render()
		})
		select {
		case <-t.C:
		case <-a.wake:
		}
	}
}

func (a *app) drawHeader(s snapshot) {
	run := 0
	for _, g := range s.Guests {
		if g.Running {
			run++
		}
	}
	ct := "[gray]conntrack n/a (root?)[-]"
	if s.CTMax > 0 {
		pct := float64(s.CTCount) / float64(s.CTMax) * 100
		ct = fmt.Sprintf("conntrack %s%s/%s (%.0f%%)[-]", level(pct), count(float64(s.CTCount)), count(float64(s.CTMax)), pct)
	}
	memPct := 0.0
	if s.HostTotal > 0 {
		memPct = float64(s.HostMem) / float64(s.HostTotal) * 100
	}
	fmt.Fprintf(a.header.Clear(),
		"[::b]lxcpeek[::-] %s  guests [aqua]%d[-] running / %d   cpu %s%.0f%%[-]   mem %s%s/%s[-]   %s\n",
		version, run, len(s.Guests), level(s.HostCPU), s.HostCPU,
		level(memPct), human(float64(s.HostMem)), human(float64(s.HostTotal)), ct)
	sortName := []string{"cpu", "mem", "net", "conns", "io", "id"}[a.sort]
	fmt.Fprintf(a.header, "[gray]sorted by %s, refresh %s, OUT/IN in bit/s seen from the guest[-]", sortName, a.interval)
}

func level(pct float64) string {
	switch {
	case pct >= 90:
		return "[red]"
	case pct >= 60:
		return "[yellow]"
	}
	return "[green]"
}

func (a *app) render() {
	gs := append([]guestStat(nil), a.last...)
	sortGuests(gs, a.sort, a.reverse)
	rows := make([]row, 0, len(gs))
	for _, g := range gs {
		r := row{key: strconv.Itoa(g.ID), cells: cells(g)}
		switch {
		case !g.Running:
			r.color = tcell.ColorGray
		case len(g.Hot) > 0:
			r.color = tcell.ColorRed
		}
		rows = append(rows, r)
	}
	a.list.setRows(rows)
}

// showConns lists where the selected guest is connecting to. That is
// usually all you need to tell a scanner or miner from a busy web server.
func (a *app) showConns() {
	id, _ := strconv.Atoi(a.list.selected())
	var g guestStat
	for _, x := range a.last {
		if x.ID == id {
			g = x
		}
	}
	if g.ID == 0 {
		return
	}
	tv := tview.NewTextView().SetDynamicColors(true).SetText("[gray]reading conntrack...[-]")
	tv.SetBorder(true).SetTitle(fmt.Sprintf(" %d %s ", g.ID, g.Name))
	done := func() {
		a.root.RemovePage("conns")
		a.modal = false
		a.SetFocus(a.list)
	}
	tv.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEscape || ev.Key() == tcell.KeyEnter || ev.Rune() == 'q' {
			done()
			return nil
		}
		return ev
	})
	grid := tview.NewGrid().SetColumns(0, 76, 0).SetRows(0, 30, 0).AddItem(tv, 1, 1, 1, 1, 0, 0, true)
	a.modal = true
	a.root.AddPage("conns", grid, true, true)
	a.SetFocus(tv)

	go func() {
		text := connReport(g.IPs)
		a.QueueUpdateDraw(func() { tv.SetText(text) })
	}()
}

func connReport(addrs []netip.Addr) string {
	if len(addrs) == 0 {
		return "No IP known for this guest. lxcpeek takes them from the net/ipconfig\nlines of the config and from the ipfilter ipsets of the guest firewall."
	}
	own := map[netip.Addr]bool{}
	for _, ip := range addrs {
		own[ip] = true
	}
	ports, peers, inPorts := map[string]int{}, map[string]int{}, map[string]int{}
	out, in := 0, 0
	err := pve.EachConn(func(c pve.Conn) {
		switch {
		case own[c.Src]:
			out++
			ports[c.Proto+"/"+c.DPort]++
			peers[c.Dst.String()]++
		case own[c.Dst]:
			in++
			inPorts[c.Proto+"/"+c.DPort]++
		}
	})
	if err != nil {
		return "[red]" + err.Error() + "[-], run lxcpeek as root on the PVE node"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[::b]%d outgoing, %d incoming[::-]   %s\n", out, in, ips(addrs))
	top(&b, "outgoing by port", ports, out)
	top(&b, "outgoing by destination", peers, out)
	top(&b, "incoming by port", inPorts, in)
	return b.String()
}

func top(b *strings.Builder, title string, m map[string]int, total int) {
	type kv struct {
		k string
		v int
	}
	var s []kv
	for k, v := range m {
		s = append(s, kv{k, v})
	}
	sort.Slice(s, func(i, j int) bool { return s[i].v > s[j].v || s[i].v == s[j].v && s[i].k < s[j].k })
	fmt.Fprintf(b, "\n[aqua]%s[-]\n", title)
	if len(s) == 0 {
		b.WriteString("  [gray]none[-]\n")
	}
	for i, e := range s {
		if i == 6 {
			fmt.Fprintf(b, "  [gray]+%d more[-]\n", len(s)-6)
			break
		}
		fmt.Fprintf(b, "  %-40s %7d  %3.0f%%\n", tview.Escape(e.k), e.v, float64(e.v)/float64(total)*100)
	}
}

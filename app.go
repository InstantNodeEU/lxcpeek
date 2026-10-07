package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
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
	snap     snapshot
	sort     sortKey
	reverse  bool
	modal    bool
	interval time.Duration
}

var (
	sortKeys  = map[rune]sortKey{'c': byCPU, 'm': byMem, 'n': byNet, 'k': byConns, 'o': byIO, 'i': byID}
	sortNames = []string{"cpu", "memory", "network", "connections", "disk io", "id"}
)

// tview color tags for the palette in list.go
const (
	tAccent = "[#f0883e]"
	tDim    = "[#6e7681]"
	tBad    = "[#f85149]"
)

var toneTag = map[tone]string{
	toneNone: "[-]", toneDim: tDim, toneGood: "[#3fb950]", toneWarn: "[#d29922]", toneBad: tBad, toneAccent: tAccent,
}

func newApp(interval time.Duration) *app {
	// draw on the terminal's own background instead of tview's black
	tview.Styles.PrimitiveBackgroundColor = tcell.ColorDefault
	tview.Styles.ContrastBackgroundColor = tcell.ColorDefault
	a := &app{
		Application: tview.NewApplication(),
		header:      tview.NewTextView().SetDynamicColors(true).SetWrap(false),
		list:        newList(len(headers)-1, headers...).alignRight(rightCols...),
		footer:      tview.NewPages(),
		hints:       tview.NewTextView().SetDynamicColors(true).SetWrap(false),
		input:       tview.NewInputField().SetLabel(" / "),
		root:        tview.NewPages(),
		interval:    interval,
	}
	a.input.SetFieldBackgroundColor(tcell.ColorDefault).SetLabelColor(colAccent)
	a.input.SetChangedFunc(a.list.setFilter)
	a.input.SetDoneFunc(func(k tcell.Key) {
		if k == tcell.KeyEscape {
			a.input.SetText("")
		}
		a.footer.SwitchToPage("hints")
		a.SetFocus(a.list)
		a.drawHints()
	})
	a.footer.AddPage("hints", a.hints, true, true)
	a.footer.AddPage("input", a.input, true, false)

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.header, 3, 0, false).
		AddItem(a.list, 0, 1, true).
		AddItem(a.footer, 1, 0, false)
	a.root.AddPage("main", layout, true, true)
	a.list.SetSelectedFunc(func(int, int) { a.showConns() })
	a.SetRoot(a.root, true).SetInputCapture(a.keys)
	a.header.SetText(" " + tAccent + "[::b]lxcpeek[::-][-] " + tDim + "reading guests...[-]")
	a.drawHints()
	return a
}

func (a *app) start() error {
	go a.loop()
	return a.Run()
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
	switch ev.Rune() {
	case 'q':
		a.Stop()
		return nil
	case '/':
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
				a.header.SetText(" " + tBad + tview.Escape(err.Error()) + "[-]")
				return
			}
			a.snap = snap
			a.render()
		})
		<-t.C
	}
}

func (a *app) render() {
	a.drawHeader()
	a.drawHints()
	gs := append([]guestStat(nil), a.snap.Guests...)
	sortGuests(gs, a.sort, a.reverse)
	rows := make([]row, 0, len(gs))
	for _, g := range gs {
		rows = append(rows, row{key: strconv.Itoa(g.ID), cells: cells(g)})
	}
	a.list.setRows(rows)
}

func (a *app) drawHeader() {
	s := a.snap
	run, hot := 0, 0
	for _, g := range s.Guests {
		if g.Running {
			run++
		}
		if len(g.Hot) > 0 {
			hot++
		}
	}
	hotText := tDim + "nothing hot[-]"
	if hot > 0 {
		hotText = tBad + fmt.Sprintf("[::b]%d hot[::-][-]", hot)
	}
	w := 120
	if _, _, iw, _ := a.header.GetInnerRect(); iw > 0 {
		w = iw
	}
	bw := max((w-66)/3, 6)

	h := a.header.Clear()
	fmt.Fprintf(h, " %s[::b]lxcpeek[::-][-] %s%s[-]   [::b]%s[::-]   %sup[-] %s   %sload[-] %.2f %.2f %.2f   %sguests[-] %d / %d   %s\n",
		tAccent, tDim, version, tview.Escape(s.Host), tDim, duration(s.Uptime), tDim, s.Load[0], s.Load[1], s.Load[2],
		tDim, run, len(s.Guests), hotText)
	ct := tDim + "n/a, run as root[-]"
	if s.CTMax > 0 {
		ct = meter(s.ctPct(), bw) + fmt.Sprintf(" %s / %s", count(float64(s.CTCount)), count(float64(s.CTMax)))
	}
	fmt.Fprintf(h, " %scpu[-] %s %3.0f%% of %d   %smem[-] %s %s / %s   %sconntrack[-] %s",
		tDim, meter(s.HostCPU, bw), s.HostCPU, s.Cores,
		tDim, meter(s.memPct(), bw), human(float64(s.HostMem)), human(float64(s.HostTotal)),
		tDim, ct)
}

// meter is a thin usage line, colored part plus a dim rest.
func meter(pct float64, width int) string {
	n := int(min(max(pct, 0), 100) / 100 * float64(width))
	return toneTag[level(pct)] + strings.Repeat("━", n) + "[#30363d]" + strings.Repeat("━", width-n) + "[-]"
}

func (a *app) drawHints() {
	dir := "↓"
	if a.reverse {
		dir = "↑"
	}
	key := func(k, what string) string { return tAccent + k + "[-] " + tDim + what + "[-]   " }
	a.hints.SetText(" " + key("c m n k o i", "sort") + key("/", "filter") + key("enter", "connections") + key("q", "quit") +
		tDim + "sorted by[-] " + sortNames[a.sort] + " " + dir)
}

// showConns lists where the selected guest is connecting to. That is
// usually all you need to tell a scanner or miner from a busy web server.
func (a *app) showConns() {
	id, _ := strconv.Atoi(a.list.selected())
	var g guestStat
	for _, x := range a.snap.Guests {
		if x.ID == id {
			g = x
		}
	}
	if g.ID == 0 {
		return
	}
	tv := tview.NewTextView().SetDynamicColors(true).SetText(tDim + " reading conntrack...[-]")
	tv.SetBorder(true).SetBorderColor(colAccent).SetTitleColor(colAccent).
		SetTitle(fmt.Sprintf(" %d %s ", g.ID, g.Name)).SetBorderPadding(0, 0, 1, 1)
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
	grid := tview.NewGrid().SetColumns(0, 86, 0).SetRows(0, 31, 0).AddItem(tv, 1, 1, 1, 1, 0, 0, true)
	a.modal = true
	a.root.AddPage("conns", grid, true, true)
	a.SetFocus(tv)

	go func() {
		text := connText(g)
		a.QueueUpdateDraw(func() {
			tv.SetText(text)
			grid.SetRows(0, strings.Count(text, "\n")+3, 0)
		})
	}()
}

func connText(g guestStat) string {
	if len(g.IPs) == 0 {
		return "No IP known for this guest. lxcpeek takes them from the net/ipconfig\nlines of the config and from the ipfilter ipsets of the guest firewall."
	}
	s, err := summarize(g.IPs)
	if err != nil {
		return tBad + err.Error() + "[-], run lxcpeek as root on the PVE node"
	}
	var b strings.Builder
	addrs := make([]string, len(g.IPs))
	for i, ip := range g.IPs {
		addrs[i] = ip.String()
	}
	fmt.Fprintf(&b, "%s%s[-]\n[::b]%d[::-] outgoing   [::b]%d[::-] incoming\n",
		tDim, strings.Join(addrs, "  "), s.Out, s.In)
	section(&b, "outgoing by port", s.Ports, s.Out, true)
	section(&b, "outgoing by destination", s.Peers, s.Out, false)
	section(&b, "incoming by port", s.InPorts, s.In, true)
	b.WriteString("\n" + tDim + "esc closes[-]")
	return b.String()
}

func section(b *strings.Builder, title string, list []kv, total int, ports bool) {
	fmt.Fprintf(b, "\n%s[::b]%s[::-][-]\n", tAccent, strings.ToUpper(title))
	if len(list) == 0 {
		b.WriteString(tDim + "none[-]\n")
		return
	}
	for i, e := range list {
		if i == 6 {
			fmt.Fprintf(b, "%s+%d more[-]\n", tDim, len(list)-6)
			break
		}
		pct := float64(e.N) / float64(total) * 100
		name, extra := "", ""
		if ports {
			name = portNames[e.Key]
			if e.Peers > 0 {
				extra = fmt.Sprintf("%s%d hosts[-]", tDim, e.Peers)
			}
			if v := verdict(e); v != "" {
				extra += "  " + tBad + "[::b]" + v + "[::-][-]"
			}
		}
		fmt.Fprintf(b, "%-22s %s%-10s[-] %s%s[-] %6d %4.0f%%  %s\n",
			tview.Escape(e.Key), tDim, name, tAccent, blocks(pct, 14), e.N, pct, extra)
	}
}

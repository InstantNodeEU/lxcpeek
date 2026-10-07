package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// ansi maps tones to escape codes, or to nothing when the output is not
// a terminal.
type ansi map[tone]string

func newANSI(on bool) ansi {
	if !on {
		return ansi{}
	}
	return ansi{
		toneDim:    "\x1b[38;2;110;118;129m",
		toneGood:   "\x1b[38;2;63;185;80m",
		toneWarn:   "\x1b[38;2;210;153;34m",
		toneBad:    "\x1b[38;2;248;81;73m",
		toneAccent: "\x1b[38;2;240;136;62m",
	}
}

func (c ansi) paint(t tone, s string) string {
	if c[t] == "" || s == "" {
		return s
	}
	return c[t] + s + "\x1b[0m"
}

func (c ansi) bold(s string) string {
	if len(c) == 0 {
		return s
	}
	return "\x1b[1m" + s + "\x1b[0m"
}

// colorOK follows https://no-color.org and only colors terminals.
func colorOK() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

const rule = "------------------------------------------------------------------------"

// report prints the -once output. With hotOnly it skips the guest table.
func report(w io.Writer, s snapshot, sampled time.Duration, c ansi, hotOnly bool, details map[int]connSummary) {
	sec := func(title string) {
		fmt.Fprintf(w, "\n%s\n%s\n", c.bold(title), c.paint(toneDim, rule))
	}
	kvLine := func(k, v string) {
		fmt.Fprintf(w, "%s : %s\n", c.paint(toneDim, fmt.Sprintf("%-10s", k)), v)
	}

	fmt.Fprintf(w, "%s %s, github.com/instantnodeeu/lxcpeek\n", c.paint(toneAccent, c.bold("lxcpeek")), version)
	fmt.Fprintf(w, "%s, sampled over %s\n", time.Now().Format("Mon Jan _2 15:04:05 MST 2006"), sampled)

	run, vms, cts := 0, 0, 0
	var hot, stopped []guestStat
	for _, g := range s.Guests {
		if g.Running {
			run++
		} else {
			stopped = append(stopped, g)
		}
		if g.Kind == "vm" {
			vms++
		} else {
			cts++
		}
		if len(g.Hot) > 0 {
			hot = append(hot, g)
		}
	}

	sec("Node")
	kvLine("Host", fmt.Sprintf("%s, up %s", s.Host, duration(s.Uptime)))
	kvLine("CPU", fmt.Sprintf("%d cores, %s busy, load %.2f %.2f %.2f",
		s.Cores, c.paint(level(s.HostCPU), fmt.Sprintf("%.0f%%", s.HostCPU)), s.Load[0], s.Load[1], s.Load[2]))
	kvLine("Memory", fmt.Sprintf("%s / %s (%s)", human(float64(s.HostMem)), human(float64(s.HostTotal)),
		c.paint(level(s.memPct()), fmt.Sprintf("%.0f%%", s.memPct()))))
	if s.CTMax > 0 {
		kvLine("Conntrack", fmt.Sprintf("%s / %s (%s)", count(float64(s.CTCount)), count(float64(s.CTMax)),
			c.paint(level(s.ctPct()), fmt.Sprintf("%.0f%%", s.ctPct()))))
	} else {
		kvLine("Conntrack", c.paint(toneDim, "not readable, run as root"))
	}
	kvLine("Guests", fmt.Sprintf("%d running, %d stopped (%d vm, %d ct)", run, len(stopped), vms, cts))

	sec(fmt.Sprintf("Hot (%d)", len(hot)))
	if len(hot) == 0 {
		fmt.Fprintln(w, c.paint(toneGood, "nothing hot"))
	}
	for _, g := range hot {
		fmt.Fprintf(w, "%-6d %-22s %s\n", g.ID, g.Name, c.paint(toneBad, reasons(g)))
		d, ok := details[g.ID]
		if !ok || len(d.Ports) == 0 {
			continue
		}
		var top []string
		for i, p := range d.Ports {
			if i == 3 {
				break
			}
			t := fmt.Sprintf("%s %.0f%%", p.Key, float64(p.N)/float64(d.Out)*100)
			if n := portNames[p.Key]; n != "" {
				t = fmt.Sprintf("%s (%s) %.0f%%", p.Key, n, float64(p.N)/float64(d.Out)*100)
			}
			if v := verdict(p); v != "" {
				t += " " + c.paint(toneBad, c.bold(v))
			}
			top = append(top, t)
		}
		fmt.Fprintf(w, "%-6s %s %s\n", "", c.paint(toneDim, "out:"), strings.Join(top, c.paint(toneDim, ", ")))
	}
	if hotOnly {
		return
	}

	sec(fmt.Sprintf("Guests, by %s", sortNames[byCPU]))
	var rows [][]cell
	for _, g := range s.Guests {
		if g.Running {
			rows = append(rows, cells(g))
		}
	}
	table(w, c, rows)
	if len(stopped) > 0 {
		names := make([]string, len(stopped))
		for i, g := range stopped {
			names[i] = fmt.Sprintf("%d %s", g.ID, g.Name)
		}
		fmt.Fprintf(w, "\n%s %s\n", c.paint(toneDim, "stopped:"), c.paint(toneDim, strings.Join(names, ", ")))
	}
	if s.CTErr != "" {
		fmt.Fprintf(w, "\n%s %s\n", c.paint(toneWarn, "connections:"), s.CTErr+", run as root")
	}
}

func reasons(g guestStat) string {
	var r []string
	for _, h := range g.Hot {
		switch h {
		case "cpu":
			r = append(r, fmt.Sprintf("cpu %.0f%%", g.CPU))
		case "mem":
			r = append(r, fmt.Sprintf("memory %.0f%%", g.memPct()))
		case "pps":
			r = append(r, fmt.Sprintf("%s packets/s out", count(g.PPSOut)))
		case "conns":
			r = append(r, fmt.Sprintf("%d connections out", g.ConnsOut))
		}
	}
	return strings.Join(r, ", ")
}

// table pads by hand because tabwriter counts escape codes as text.
func table(w io.Writer, c ansi, rows [][]cell) {
	right := map[int]bool{}
	for _, i := range rightCols {
		right[i] = true
	}
	width := make([]int, len(headers))
	for i, h := range headers {
		width[i] = len(h)
	}
	for _, r := range rows {
		for i, x := range r {
			width[i] = max(width[i], utf8.RuneCountInString(x.text))
		}
	}
	line := func(r []cell) {
		var b strings.Builder
		for i, x := range r {
			pad := strings.Repeat(" ", width[i]-utf8.RuneCountInString(x.text))
			if i > 0 {
				b.WriteString("  ")
			}
			if right[i] {
				b.WriteString(pad + c.paint(x.tone, x.text))
			} else {
				b.WriteString(c.paint(x.tone, x.text) + pad)
			}
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
	hd := make([]cell, len(headers))
	for i, h := range headers {
		hd[i] = cell{h, toneDim}
	}
	line(hd)
	for _, r := range rows {
		line(r)
	}
}

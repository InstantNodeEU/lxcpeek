package main

import (
	"fmt"
	"net/netip"
	"strings"
)

// tone is a color without saying how to draw it, so the TUI and the
// plain report can share the table code.
type tone int

const (
	toneNone tone = iota
	toneDim
	toneGood
	toneWarn
	toneBad
	toneAccent
)

// level colors a usage percentage.
func level(pct float64) tone {
	switch {
	case pct >= hotPct:
		return toneBad
	case pct >= 60:
		return toneWarn
	}
	return toneGood
}

// near colors a value against its hot threshold, quiet until halfway.
func near(v, limit float64) tone {
	switch {
	case v >= limit:
		return toneBad
	case v >= limit/2:
		return toneWarn
	}
	return toneNone
}

func human(b float64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%.0fB", b)
	}
	for _, s := range []string{"K", "M", "G", "T", "P"} {
		b /= unit
		if b < unit {
			if b >= 100 {
				return fmt.Sprintf("%.0f%s", b, s)
			}
			return fmt.Sprintf("%.1f%s", b, s)
		}
	}
	return fmt.Sprintf("%.0fE", b/unit)
}

// bits shows network rates the way hosters talk about them.
func bits(bytesPerSec float64) string {
	b := bytesPerSec * 8
	switch {
	case b >= 1e9:
		return fmt.Sprintf("%.2fG", b/1e9)
	case b >= 1e6:
		return fmt.Sprintf("%.1fM", b/1e6)
	case b >= 1e3:
		return fmt.Sprintf("%.0fk", b/1e3)
	}
	return fmt.Sprintf("%.0f", b)
}

func count(n float64) string {
	switch {
	case n >= 1e6:
		return fmt.Sprintf("%.1fM", n/1e6)
	case n >= 1e4:
		return fmt.Sprintf("%.0fk", n/1e3)
	}
	return fmt.Sprintf("%.0f", n)
}

func duration(sec float64) string {
	s := int64(sec)
	d, h, m := s/86400, s/3600%24, s/60%60
	if d > 0 {
		return fmt.Sprintf("%dd %dh %dm", d, h, m)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// blocks draws pct as a bar of width cells with eighth block precision.
func blocks(pct float64, width int) string {
	eighths := int(min(max(pct, 0), 100) / 100 * float64(width*8))
	s := strings.Repeat("█", eighths/8)
	if r := eighths % 8; r > 0 {
		s += string([]rune(" ▏▎▍▌▋▊▉")[r])
	}
	return s + strings.Repeat(" ", width-len([]rune(s)))
}

func ips(a []netip.Addr) string {
	if len(a) == 0 {
		return "-"
	}
	s := a[0].String()
	if len(a) > 1 {
		s += fmt.Sprintf(" +%d", len(a)-1)
	}
	return s
}

type cell struct {
	text string
	tone tone
}

var headers = []string{"ID", "", "NAME", "IP", "CPU", "MEM", "OUT", "IN", "PPS", "IO/s", "CONN", "HOT"}

// rightCols are the numeric columns.
var rightCols = []int{0, 4, 5, 6, 7, 8, 9, 10}

// cells is one table row, shared by the TUI and the report.
func cells(g guestStat) []cell {
	if !g.Running {
		row := []cell{{fmt.Sprint(g.ID), toneDim}, {g.Kind, toneDim}, {g.Name, toneDim}, {ips(g.IPs), toneDim}}
		for range 7 {
			row = append(row, cell{})
		}
		return append(row, cell{"stopped", toneDim})
	}
	mem := cell{human(float64(g.Mem)), toneNone}
	if g.MemMax > 0 {
		mem.text += " / " + human(float64(g.MemMax))
		if t := level(g.memPct()); g.Kind == "ct" && t != toneGood {
			mem.tone = t
		}
	}
	io := cell{"-", toneDim}
	if g.IOKnown {
		io = cell{human(g.IORead + g.IOWrite), toneNone}
	}
	name := toneNone
	if len(g.Hot) > 0 {
		name = toneBad
	}
	return []cell{
		{fmt.Sprint(g.ID), name},
		{g.Kind, toneDim},
		{g.Name, name},
		{ips(g.IPs), toneDim},
		{blocks(g.CPU, 6) + fmt.Sprintf("%4.0f%%", g.CPU), level(g.CPU)},
		mem,
		{bits(g.Out), toneNone},
		{bits(g.In), toneNone},
		{count(g.PPSOut), near(g.PPSOut, hotPPS)},
		io,
		{fmt.Sprintf("%d / %d", g.ConnsOut, g.ConnsIn), near(float64(g.ConnsOut), hotConns)},
		{strings.Join(g.Hot, " "), toneBad},
	}
}

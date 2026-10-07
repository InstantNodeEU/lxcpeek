package main

import (
	"fmt"
	"net/netip"
	"strings"
)

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

// cells is one table row, shared by the TUI and -once output.
func cells(g guestStat) []string {
	if !g.Running {
		return []string{fmt.Sprint(g.ID), g.Kind, g.Name, ips(g.IPs), "", "", "", "", "", "", "", "stopped"}
	}
	mem := human(float64(g.Mem))
	if g.MemMax > 0 {
		mem += "/" + human(float64(g.MemMax))
	}
	return []string{
		fmt.Sprint(g.ID), g.Kind, g.Name, ips(g.IPs),
		fmt.Sprintf("%.0f%%", g.CPU),
		mem,
		bits(g.Out), bits(g.In),
		count(g.PPSOut),
		human(g.IORead + g.IOWrite),
		fmt.Sprintf("%d/%d", g.ConnsOut, g.ConnsIn),
		strings.Join(g.Hot, " "),
	}
}

var headers = []string{"ID", "", "NAME", "IP", "CPU", "MEM", "OUT", "IN", "PPS", "IO/s", "CONN o/i", "!"}

// Package pve reads guest configs and live stats straight from a Proxmox VE
// host: /etc/pve, cgroup v2, /sys/class/net and the conntrack table.
package pve

import (
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Roots, overridden in tests.
var (
	EtcPVE = "/etc/pve"
	Cgroup = "/sys/fs/cgroup"
	SysNet = "/sys/class/net"
	Proc   = "/proc"
	Run    = "/var/run"
)

type Guest struct {
	ID       int          `json:"id"`
	Kind     string       `json:"kind"` // "ct" or "vm"
	Name     string       `json:"name"`
	IPs      []netip.Addr `json:"ips"`
	Cores    float64      `json:"cores"`     // 0 means not limited
	MemLimit uint64       `json:"mem_limit"` // bytes, from the config
}

// ReadGuests returns all containers and VMs configured on this node.
func ReadGuests() ([]Guest, error) {
	var out []Guest
	for _, d := range []struct{ dir, kind string }{{"lxc", "ct"}, {"qemu-server", "vm"}} {
		files, err := filepath.Glob(filepath.Join(EtcPVE, d.dir, "*.conf"))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			id, err := strconv.Atoi(strings.TrimSuffix(filepath.Base(f), ".conf"))
			if err != nil {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			g := parseConf(d.kind, id, string(b))
			if fw, err := os.ReadFile(filepath.Join(EtcPVE, "firewall", strconv.Itoa(id)+".fw")); err == nil {
				for _, ip := range parseIPFilter(string(fw)) {
					g.addIP(ip)
				}
			}
			out = append(out, g)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func parseConf(kind string, id int, s string) Guest {
	g := Guest{ID: id, Kind: kind}
	sockets, cpulimit := 1.0, 0.0
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "[") {
			break // snapshots follow, they describe old state
		}
		k, v, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		switch {
		case k == "hostname" || k == "name":
			g.Name = v
		case k == "memory":
			if mb, err := strconv.ParseUint(v, 10, 64); err == nil {
				g.MemLimit = mb << 20
			}
		case k == "cores":
			g.Cores, _ = strconv.ParseFloat(v, 64)
		case k == "sockets":
			sockets, _ = strconv.ParseFloat(v, 64)
		case k == "cpulimit":
			cpulimit, _ = strconv.ParseFloat(v, 64)
		case strings.HasPrefix(k, "net") && kind == "ct", strings.HasPrefix(k, "ipconfig"):
			for _, opt := range strings.Split(v, ",") {
				ok, ov, _ := strings.Cut(opt, "=")
				if ok == "ip" || ok == "ip6" {
					if p, err := netip.ParsePrefix(ov); err == nil {
						g.addIP(p.Addr())
					}
				}
			}
		}
	}
	if kind == "vm" {
		if g.Cores == 0 {
			g.Cores = 1
		}
		g.Cores *= max(sockets, 1)
	}
	if cpulimit > 0 && (g.Cores == 0 || cpulimit < g.Cores) {
		g.Cores = cpulimit
	}
	return g
}

// parseIPFilter pulls addresses out of the ipfilter-netN ipsets in a guest
// firewall file. VMs without cloud-init only have their IP there.
func parseIPFilter(s string) []netip.Addr {
	var out []netip.Addr
	in := false
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			in = strings.HasPrefix(strings.ToUpper(line), "[IPSET IPFILTER-NET")
			continue
		}
		if !in || line == "" || line[0] == '#' || line[0] == '!' {
			continue
		}
		f := strings.Fields(line)[0]
		if a, err := netip.ParseAddr(f); err == nil {
			out = append(out, a)
		} else if p, err := netip.ParsePrefix(f); err == nil && p.IsSingleIP() {
			out = append(out, p.Addr())
		}
	}
	return out
}

func (g *Guest) addIP(a netip.Addr) {
	a = a.Unmap()
	for _, x := range g.IPs {
		if x == a {
			return
		}
	}
	g.IPs = append(g.IPs, a)
}

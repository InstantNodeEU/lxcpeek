package main

import (
	"net/netip"
	"sort"

	"github.com/instantnodeeu/lxcpeek/internal/pve"
)

type kv struct {
	Key   string
	N     int
	Peers int // distinct remote addresses, only for outgoing ports
}

type connSummary struct {
	Out, In int
	Ports   []kv // outgoing by proto/port
	Peers   []kv // outgoing by destination
	InPorts []kv // incoming by proto/port
}

// summarize reads the conntrack table once and groups everything the
// given addresses take part in.
func summarize(addrs []netip.Addr) (connSummary, error) {
	own := map[netip.Addr]bool{}
	for _, ip := range addrs {
		own[ip] = true
	}
	var s connSummary
	ports, peers, inPorts := map[string]int{}, map[string]int{}, map[string]int{}
	portPeers := map[string]map[netip.Addr]struct{}{}
	err := pve.EachConn(func(c pve.Conn) {
		switch {
		case own[c.Src]:
			s.Out++
			p := c.Proto + "/" + c.DPort
			ports[p]++
			peers[c.Dst.String()]++
			if portPeers[p] == nil {
				portPeers[p] = map[netip.Addr]struct{}{}
			}
			portPeers[p][c.Dst] = struct{}{}
		case own[c.Dst]:
			s.In++
			inPorts[c.Proto+"/"+c.DPort]++
		}
	})
	s.Ports = ranked(ports)
	for i := range s.Ports {
		s.Ports[i].Peers = len(portPeers[s.Ports[i].Key])
	}
	s.Peers = ranked(peers)
	s.InPorts = ranked(inPorts)
	return s, err
}

func ranked(m map[string]int) []kv {
	out := make([]kv, 0, len(m))
	for k, n := range m {
		out = append(out, kv{Key: k, N: n})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].N > out[j].N || out[i].N == out[j].N && out[i].Key < out[j].Key
	})
	return out
}

var portNames = map[string]string{
	"tcp/22": "ssh", "tcp/23": "telnet", "tcp/2323": "telnet", "tcp/25": "smtp",
	"tcp/53": "dns", "udp/53": "dns", "tcp/80": "http", "tcp/443": "https", "udp/443": "quic",
	"udp/123": "ntp", "tcp/445": "smb", "tcp/465": "smtps", "tcp/587": "submission",
	"tcp/1433": "mssql", "tcp/2375": "docker", "tcp/3306": "mysql", "tcp/3389": "rdp",
	"tcp/5432": "postgres", "tcp/5900": "vnc", "tcp/6379": "redis", "tcp/8080": "http-alt",
	"tcp/3333": "stratum", "tcp/14433": "stratum", "tcp/14444": "stratum", "tcp/45700": "stratum",
	"udp/51820": "wireguard", "udp/1194": "openvpn", "tcp/25565": "minecraft", "udp/27015": "source",
}

// Lots of different destinations is normal for these.
var webPorts = map[string]bool{"tcp/80": true, "tcp/443": true, "udp/443": true, "tcp/53": true, "udp/53": true, "udp/123": true}

// verdict is a guess, shown next to a port, of what a guest is doing.
// ponytail: three rules that catch what we actually see on our nodes;
// a real rule table when they start to misfire.
func verdict(p kv) string {
	switch {
	case portNames[p.Key] == "stratum":
		return "mining pool?"
	case (p.Key == "tcp/25" || p.Key == "tcp/465" || p.Key == "tcp/587") && p.Peers >= 50:
		return "spam?"
	case p.Peers >= 100 && !webPorts[p.Key]:
		return "scan?"
	}
	return ""
}

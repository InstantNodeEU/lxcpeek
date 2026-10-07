package pve

import (
	"net/netip"
	"testing"
)

func TestParseConf(t *testing.T) {
	ct := parseConf("ct", 101, `arch: amd64
cores: 4
cpulimit: 2.5
hostname: web1
memory: 2048
net0: name=eth0,bridge=vmbr0,hwaddr=BC:24:11:00:00:01,ip=94.249.230.50/24,gw=94.249.230.1,ip6=2a0e::50/64,type=veth
net1: name=eth1,bridge=vmbr1,ip=dhcp

[snap1]
hostname: old
memory: 512
`)
	if ct.Name != "web1" || ct.MemLimit != 2048<<20 || ct.Cores != 2.5 {
		t.Errorf("ct = %+v", ct)
	}
	if len(ct.IPs) != 2 || ct.IPs[0] != netip.MustParseAddr("94.249.230.50") || ct.IPs[1] != netip.MustParseAddr("2a0e::50") {
		t.Errorf("ct ips = %v", ct.IPs)
	}

	vm := parseConf("vm", 200, "name: win\ncores: 2\nsockets: 2\nmemory: 8192\nipconfig0: ip=10.0.0.5/24,gw=10.0.0.1\n")
	if vm.Cores != 4 || vm.Name != "win" || len(vm.IPs) != 1 {
		t.Errorf("vm = %+v", vm)
	}
	if bare := parseConf("vm", 201, "memory: 1024\n"); bare.Cores != 1 {
		t.Errorf("vm default cores = %v", bare.Cores)
	}
	if bare := parseConf("ct", 102, "memory: 1024\n"); bare.Cores != 0 {
		t.Errorf("ct without cores should be unlimited, got %v", bare.Cores)
	}
}

func TestParseIPFilter(t *testing.T) {
	got := parseIPFilter(`[OPTIONS]
enable: 1

[IPSET ipfilter-net0]
94.249.230.61
94.249.230.62/32 # second
!10.0.0.0/8
10.1.0.0/16

[RULES]
IN ACCEPT -p tcp -dport 22
`)
	if len(got) != 2 || got[1] != netip.MustParseAddr("94.249.230.62") {
		t.Errorf("got %v", got)
	}
}

func TestGuestIface(t *testing.T) {
	for name, want := range map[string]bool{
		"veth101i0": true, "tap101i12": true, "veth1011i0": false,
		"veth101i": false, "fwpr101p0": false, "tap10i0": false, "veth101i0x": false,
	} {
		if guestIface(name, 101) != want {
			t.Errorf("%s: want %v", name, want)
		}
	}
}

func TestParseConn(t *testing.T) {
	c, ok := parseConn("ipv4     2 tcp      6 117 ESTABLISHED src=94.249.230.50 dst=1.1.1.1 sport=51234 dport=443 src=1.1.1.1 dst=94.249.230.50 sport=443 dport=51234 [ASSURED] mark=0 zone=0 use=2")
	if !ok || c.Proto != "tcp" || c.Src.String() != "94.249.230.50" || c.Dst.String() != "1.1.1.1" || c.DPort != "443" {
		t.Errorf("tcp = %+v %v", c, ok)
	}
	c, ok = parseConn("ipv6     10 udp      17 29 src=2a0e:0000:0000:0000:0000:0000:0000:0050 dst=2606:4700:4700:0000:0000:0000:0000:1111 sport=5353 dport=53 [UNREPLIED] src=2606:4700:4700:0000:0000:0000:0000:1111 dst=2a0e:0000:0000:0000:0000:0000:0000:0050 sport=53 dport=5353 mark=0 zone=0 use=2")
	if !ok || c.Src != netip.MustParseAddr("2a0e::50") || c.DPort != "53" {
		t.Errorf("udp6 = %+v %v", c, ok)
	}
	if _, ok := parseConn("ipv4 2 icmp"); ok {
		t.Error("short line parsed")
	}
}

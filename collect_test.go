package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/instantnodeeu/lxcpeek/internal/pve"
)

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeNode builds a tiny PVE host: CT 101 (2 cores, 1G) misbehaving,
// VM 200 idle with all its memory touched.
func fakeNode(t *testing.T) string {
	d := t.TempDir()
	pve.EtcPVE, pve.Cgroup, pve.SysNet, pve.Proc, pve.Run = d+"/pve", d+"/cg", d+"/net", d+"/proc", d+"/run"
	write(t, d+"/pve/lxc/101.conf", "hostname: scanner\ncores: 2\nmemory: 1024\nnet0: name=eth0,ip=94.249.230.50/24\n")
	write(t, d+"/pve/qemu-server/200.conf", "name: idle\nmemory: 2048\n")
	write(t, d+"/pve/firewall/200.fw", "[IPSET ipfilter-net0]\n94.249.230.60\n")
	write(t, d+"/cg/lxc/101/memory.current", "1000000000\n")
	write(t, d+"/cg/lxc/101/memory.stat", "anon 900\ninactive_file 100000000\n")
	write(t, d+"/cg/lxc/101/memory.max", "max\n")
	write(t, d+"/cg/lxc/101/io.stat", "8:0 rbytes=0 wbytes=0 rios=0 wios=0\n")
	write(t, d+"/cg/qemu.slice/200.scope/memory.current", "2147483648\n")
	write(t, d+"/run/qemu-server/200.pid", "4242\n")
	write(t, d+"/proc/4242/io", "rchar: 1\nread_bytes: 4096\nwrite_bytes: 8192\n")
	write(t, d+"/proc/stat", "cpu  100 0 100 800 0 0 0 0 0 0\n")
	write(t, d+"/proc/meminfo", "MemTotal: 1000 kB\nMemAvailable: 250 kB\n")
	write(t, d+"/proc/sys/net/netfilter/nf_conntrack_count", "3\n")
	write(t, d+"/proc/sys/net/netfilter/nf_conntrack_max", "262144\n")
	var ct string
	for range hotConns {
		ct += "ipv4 2 tcp 6 10 SYN_SENT src=94.249.230.50 dst=10.9.9.9 sport=1 dport=23 [UNREPLIED] src=10.9.9.9 dst=94.249.230.50 sport=23 dport=1 mark=0 use=1\n"
	}
	ct += "ipv4 2 tcp 6 10 ESTABLISHED src=8.8.8.8 dst=94.249.230.60 sport=1 dport=3389 src=94.249.230.60 dst=8.8.8.8 sport=3389 dport=1 mark=0 use=1\n"
	write(t, d+"/proc/net/nf_conntrack", ct)
	setCounters(t, d, 0, 0, 0)
	return d
}

func setCounters(t *testing.T, d string, usec, rxBytes, rxPkts int) {
	write(t, d+"/cg/lxc/101/cpu.stat", "usage_usec "+strconv.Itoa(usec)+"\nuser_usec 0\n")
	write(t, d+"/net/veth101i0/statistics/rx_bytes", strconv.Itoa(rxBytes))
	write(t, d+"/net/veth101i0/statistics/tx_bytes", "0")
	write(t, d+"/net/veth101i0/statistics/rx_packets", strconv.Itoa(rxPkts))
	write(t, d+"/net/veth101i0/statistics/tx_packets", "0")
}

func TestCollect(t *testing.T) {
	d := fakeNode(t)
	var c collector
	if _, err := c.collect(); err != nil {
		t.Fatal(err)
	}
	c.prevAt = time.Now().Add(-time.Second)
	// 1.9 cores busy, 10 MB/s and 50k pps out over ~1s
	setCounters(t, d, 1_900_000, 10_000_000, 50_000)
	write(t, d+"/proc/stat", "cpu  200 0 200 1400 0 0 0 0 0 0\n")
	snap, err := c.collect()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Guests) != 2 {
		t.Fatalf("guests = %d", len(snap.Guests))
	}
	ct, vm := snap.Guests[0], snap.Guests[1]
	if !ct.Running || !vm.Running {
		t.Fatalf("running: ct %v vm %v", ct.Running, vm.Running)
	}
	if ct.CPU < 85 || ct.CPU > 96 {
		t.Errorf("ct cpu = %.1f%%, want about 95", ct.CPU)
	}
	if ct.Mem != 900000000 || ct.MemMax != 1024<<20 {
		t.Errorf("ct mem = %d/%d", ct.Mem, ct.MemMax)
	}
	if ct.PPSOut < 40000 || ct.Out < 8e6 {
		t.Errorf("ct net = %.0f B/s, %.0f pps", ct.Out, ct.PPSOut)
	}
	if ct.ConnsOut != hotConns || vm.ConnsIn != 1 {
		t.Errorf("conns ct out %d, vm in %d", ct.ConnsOut, vm.ConnsIn)
	}
	if got := len(ct.Hot); got != 3 { // cpu pps conns
		t.Errorf("hot = %v", ct.Hot)
	}
	if !vm.IOKnown || len(vm.Hot) != 0 {
		t.Errorf("vm io known %v, hot %v (full VM memory must not flag)", vm.IOKnown, vm.Hot)
	}
	if snap.HostCPU != 25 || snap.HostMem != 750<<10 || snap.CTMax != 262144 {
		t.Errorf("host = %+v", snap)
	}

	rep := connReport(ct.IPs)
	for _, want := range []string{"1000 outgoing", "tcp/23", "10.9.9.9"} {
		if !strings.Contains(rep, want) {
			t.Errorf("report missing %q:\n%s", want, rep)
		}
	}
}

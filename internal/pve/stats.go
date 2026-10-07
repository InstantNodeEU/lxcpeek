package pve

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Sample holds cumulative counters for one guest at one point in time.
type Sample struct {
	Running bool
	CPUUsec uint64
	Mem     uint64 // working set: memory.current minus inactive file cache
	MemMax  uint64 // cgroup limit, 0 if none
	IORead  uint64
	IOWrite uint64
	IOKnown bool
	// Seen from the guest: what the host port receives the guest sent.
	NetOut, NetIn uint64
	PktOut, PktIn uint64
}

func cgroupDir(g Guest) string {
	if g.Kind == "ct" {
		return filepath.Join(Cgroup, "lxc", strconv.Itoa(g.ID))
	}
	return filepath.Join(Cgroup, "qemu.slice", strconv.Itoa(g.ID)+".scope")
}

// ReadSample reads the guest's cgroup and host side network ports.
// A guest without a cgroup is not running and gets a zero Sample.
func ReadSample(g Guest, ifaces []string) Sample {
	dir := cgroupDir(g)
	if _, err := os.Stat(dir); err != nil {
		return Sample{}
	}
	s := Sample{Running: true}
	s.CPUUsec = keyed(filepath.Join(dir, "cpu.stat"))["usage_usec"]
	s.Mem = readUint(filepath.Join(dir, "memory.current"))
	if f := keyed(filepath.Join(dir, "memory.stat"))["inactive_file"]; f < s.Mem {
		s.Mem -= f
	}
	s.MemMax = readUint(filepath.Join(dir, "memory.max")) // "max" parses to 0

	if b, err := os.ReadFile(filepath.Join(dir, "io.stat")); err == nil {
		s.IOKnown = true
		for _, f := range strings.Fields(string(b)) {
			k, v, ok := strings.Cut(f, "=")
			if !ok {
				continue
			}
			n, _ := strconv.ParseUint(v, 10, 64)
			switch k {
			case "rbytes":
				s.IORead += n
			case "wbytes":
				s.IOWrite += n
			}
		}
	}

	if !s.IOKnown && g.Kind == "vm" {
		// PVE does not enable the io controller on qemu.slice, but the
		// QEMU process does all the disk IO of the VM itself.
		pid := strings.TrimSpace(readString(filepath.Join(Run, "qemu-server", strconv.Itoa(g.ID)+".pid")))
		if pid != "" {
			if m := keyed(filepath.Join(Proc, pid, "io")); len(m) > 0 {
				s.IORead, s.IOWrite, s.IOKnown = m["read_bytes:"], m["write_bytes:"], true
			}
		}
	}

	for _, name := range ifaces {
		if !guestIface(name, g.ID) {
			continue
		}
		st := filepath.Join(SysNet, name, "statistics")
		s.NetOut += readUint(filepath.Join(st, "rx_bytes"))
		s.NetIn += readUint(filepath.Join(st, "tx_bytes"))
		s.PktOut += readUint(filepath.Join(st, "rx_packets"))
		s.PktIn += readUint(filepath.Join(st, "tx_packets"))
	}
	return s
}

// Ifaces lists host network interfaces once per refresh.
func Ifaces() []string {
	ents, _ := os.ReadDir(SysNet)
	out := make([]string, 0, len(ents))
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

// guestIface matches the host side ports PVE creates: veth<id>i<n> for
// containers, tap<id>i<n> for VMs.
func guestIface(name string, id int) bool {
	for _, p := range []string{"veth", "tap"} {
		rest, ok := strings.CutPrefix(name, p+strconv.Itoa(id)+"i")
		if ok && rest != "" && strings.Trim(rest, "0123456789") == "" {
			return true
		}
	}
	return false
}

func readString(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

func readUint(path string) uint64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	return n
}

// keyed parses "key value" files like cpu.stat and memory.stat.
func keyed(path string) map[string]uint64 {
	m := map[string]uint64{}
	f, err := os.Open(path)
	if err != nil {
		return m
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), " ")
		if ok {
			m[k], _ = strconv.ParseUint(v, 10, 64)
		}
	}
	return m
}

// Host CPU busy/total jiffies from the first line of /proc/stat.
func HostCPU() (busy, total uint64) {
	f, err := os.Open(filepath.Join(Proc, "stat"))
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return 0, 0
	}
	fs := strings.Fields(sc.Text())
	for i := 1; i < len(fs) && i <= 8; i++ {
		n, _ := strconv.ParseUint(fs[i], 10, 64)
		total += n
		if i != 4 && i != 5 { // idle, iowait
			busy += n
		}
	}
	return busy, total
}

// HostMem returns total and used (total minus available) bytes.
func HostMem() (total, used uint64) {
	f, err := os.Open(filepath.Join(Proc, "meminfo"))
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	var avail uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) < 2 {
			continue
		}
		n, _ := strconv.ParseUint(fs[1], 10, 64)
		switch fs[0] {
		case "MemTotal:":
			total = n << 10
		case "MemAvailable:":
			avail = n << 10
		}
	}
	return total, total - min(avail, total)
}

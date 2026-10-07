package pve

import (
	"bufio"
	"errors"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Conn is the original direction of one conntrack entry.
type Conn struct {
	Proto string
	Src   netip.Addr
	Dst   netip.Addr
	DPort string
}

// ErrNoConntrack means neither /proc/net/nf_conntrack nor the conntrack
// binary is available (module not loaded, or not running as root).
var ErrNoConntrack = errors.New("conntrack not readable")

// EachConn calls fn for every entry in the conntrack table.
func EachConn(fn func(Conn)) error {
	var r io.Reader
	if f, err := os.Open(filepath.Join(Proc, "net", "nf_conntrack")); err == nil {
		defer f.Close()
		r = f
	} else if path, err := exec.LookPath("conntrack"); err == nil {
		// Kernels without CONFIG_NF_CONNTRACK_PROCFS. Same line format.
		out, err := exec.Command(path, "-L", "-o", "extended").Output()
		if err != nil {
			return ErrNoConntrack
		}
		r = strings.NewReader(string(out))
	} else {
		return ErrNoConntrack
	}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		if c, ok := parseConn(sc.Text()); ok {
			fn(c)
		}
	}
	return sc.Err()
}

// parseConn takes the first src=, dst= and dport= of a line, which belong
// to the original direction:
//
//	ipv4 2 tcp 6 117 ESTABLISHED src=10.0.0.2 dst=1.1.1.1 sport=5123 dport=443 src=1.1.1.1 ...
func parseConn(line string) (c Conn, ok bool) {
	f := strings.Fields(line)
	if len(f) < 3 {
		return c, false
	}
	c.Proto = f[2]
	for _, x := range f[3:] {
		k, v, found := strings.Cut(x, "=")
		if !found {
			continue
		}
		switch {
		case k == "src" && !c.Src.IsValid():
			c.Src, _ = netip.ParseAddr(v)
		case k == "dst" && !c.Dst.IsValid():
			c.Dst, _ = netip.ParseAddr(v)
		case k == "dport" && c.DPort == "":
			c.DPort = v
		}
		if c.Src.IsValid() && c.Dst.IsValid() && c.DPort != "" {
			break
		}
	}
	c.Src, c.Dst = c.Src.Unmap(), c.Dst.Unmap()
	return c, c.Src.IsValid() && c.Dst.IsValid()
}

// ConntrackUsage returns the current entry count and the table size.
func ConntrackUsage() (count, max uint64) {
	d := filepath.Join(Proc, "sys", "net", "netfilter")
	return readUint(filepath.Join(d, "nf_conntrack_count")), readUint(filepath.Join(d, "nf_conntrack_max"))
}

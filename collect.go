package main

import (
	"net/netip"
	"os"
	"runtime"
	"sort"
	"time"

	"github.com/instantnodeeu/lxcpeek/internal/pve"
)

// ponytail: fixed thresholds, flags if someone needs different ones
const (
	hotPct   = 90    // cpu or memory, percent of the guest's allotment
	hotPPS   = 20000 // outgoing packets per second
	hotConns = 1000  // outgoing conntrack entries
)

type guestStat struct {
	pve.Guest
	Running  bool     `json:"running"`
	CPU      float64  `json:"cpu_pct"`   // of allotted cores
	CPUCores float64  `json:"cpu_cores"` // cores in use
	Mem      uint64   `json:"mem"`
	MemMax   uint64   `json:"mem_max"`
	Out      float64  `json:"net_out"` // bytes/s
	In       float64  `json:"net_in"`
	PPSOut   float64  `json:"pps_out"`
	IORead   float64  `json:"io_read"`
	IOWrite  float64  `json:"io_write"`
	IOKnown  bool     `json:"io_known"`
	ConnsOut int      `json:"conns_out"`
	ConnsIn  int      `json:"conns_in"`
	Hot      []string `json:"hot,omitempty"`
}

func (s snapshot) memPct() float64 {
	if s.HostTotal == 0 {
		return 0
	}
	return float64(s.HostMem) / float64(s.HostTotal) * 100
}

func (s snapshot) ctPct() float64 {
	if s.CTMax == 0 {
		return 0
	}
	return float64(s.CTCount) / float64(s.CTMax) * 100
}

func (g *guestStat) markHot() {
	g.Hot = nil
	if g.CPU >= hotPct {
		g.Hot = append(g.Hot, "cpu")
	}
	// A VM's QEMU keeps every page the guest ever touched, so from the
	// host nearly every VM looks full. Only containers get flagged.
	if g.Kind == "ct" && g.memPct() >= hotPct {
		g.Hot = append(g.Hot, "mem")
	}
	if g.PPSOut >= hotPPS {
		g.Hot = append(g.Hot, "pps")
	}
	if g.ConnsOut >= hotConns {
		g.Hot = append(g.Hot, "conns")
	}
}

func (s guestStat) memPct() float64 {
	if s.MemMax == 0 {
		return 0
	}
	return float64(s.Mem) / float64(s.MemMax) * 100
}

type snapshot struct {
	Host      string      `json:"host"`
	Cores     int         `json:"host_cores"`
	Load      [3]float64  `json:"host_load"`
	Uptime    float64     `json:"host_uptime"`
	Guests    []guestStat `json:"guests"`
	HostCPU   float64     `json:"host_cpu_pct"`
	HostMem   uint64      `json:"host_mem"`
	HostTotal uint64      `json:"host_mem_total"`
	CTCount   uint64      `json:"conntrack_count"`
	CTMax     uint64      `json:"conntrack_max"`
	CTErr     string      `json:"conntrack_error,omitempty"`
}

type collector struct {
	prev      map[int]pve.Sample
	prevAt    time.Time
	prevBusy  uint64
	prevTotal uint64
}

func (c *collector) collect() (snapshot, error) {
	guests, err := pve.ReadGuests()
	if err != nil {
		return snapshot{}, err
	}
	now := time.Now()
	dt := now.Sub(c.prevAt).Seconds()
	first := c.prev == nil
	ifaces := pve.Ifaces()

	var snap snapshot
	cur := make(map[int]pve.Sample, len(guests))
	byIP := map[netip.Addr]int{}
	for i, g := range guests {
		s := pve.ReadSample(g, ifaces)
		cur[g.ID] = s
		st := guestStat{Guest: g, Running: s.Running, Mem: s.Mem, MemMax: s.MemMax, IOKnown: s.IOKnown}
		if st.MemMax == 0 || (g.MemLimit > 0 && g.MemLimit < st.MemMax) {
			st.MemMax = g.MemLimit
		}
		if p, ok := c.prev[g.ID]; ok && s.Running && p.Running && dt > 0 {
			st.CPUCores = float64(delta(s.CPUUsec, p.CPUUsec)) / 1e6 / dt
			cores := g.Cores
			if cores == 0 {
				cores = float64(runtime.NumCPU())
			}
			st.CPU = st.CPUCores / cores * 100
			st.Out = float64(delta(s.NetOut, p.NetOut)) / dt
			st.In = float64(delta(s.NetIn, p.NetIn)) / dt
			st.PPSOut = float64(delta(s.PktOut, p.PktOut)) / dt
			st.IORead = float64(delta(s.IORead, p.IORead)) / dt
			st.IOWrite = float64(delta(s.IOWrite, p.IOWrite)) / dt
		}
		for _, ip := range g.IPs {
			byIP[ip] = i
		}
		snap.Guests = append(snap.Guests, st)
	}

	if err := pve.EachConn(func(cn pve.Conn) {
		if i, ok := byIP[cn.Src]; ok {
			snap.Guests[i].ConnsOut++
		}
		if i, ok := byIP[cn.Dst]; ok {
			snap.Guests[i].ConnsIn++
		}
	}); err != nil {
		snap.CTErr = err.Error()
	}

	for i := range snap.Guests {
		snap.Guests[i].markHot()
	}

	busy, total := pve.HostCPU()
	if total > c.prevTotal && !first {
		snap.HostCPU = float64(delta(busy, c.prevBusy)) / float64(total-c.prevTotal) * 100
	}
	snap.HostTotal, snap.HostMem = pve.HostMem()
	snap.Host, _ = os.Hostname()
	snap.Cores = runtime.NumCPU()
	snap.Load = pve.HostLoad()
	snap.Uptime = pve.HostUptime()
	snap.CTCount, snap.CTMax = pve.ConntrackUsage()

	c.prev, c.prevAt, c.prevBusy, c.prevTotal = cur, now, busy, total
	return snap, nil
}

// delta guards against counters that went backwards (guest restarted).
func delta(a, b uint64) uint64 {
	if a < b {
		return 0
	}
	return a - b
}

type sortKey int

const (
	byCPU sortKey = iota
	byMem
	byNet
	byConns
	byIO
	byID
)

func sortGuests(gs []guestStat, k sortKey, reverse bool) {
	less := func(a, b guestStat) bool {
		switch k {
		case byMem:
			return a.Mem > b.Mem
		case byNet:
			return a.Out+a.In > b.Out+b.In
		case byConns:
			return a.ConnsOut+a.ConnsIn > b.ConnsOut+b.ConnsIn
		case byIO:
			return a.IORead+a.IOWrite > b.IORead+b.IOWrite
		case byID:
			return a.ID < b.ID
		}
		if a.CPU != b.CPU {
			return a.CPU > b.CPU
		}
		return a.CPUCores > b.CPUCores
	}
	sort.SliceStable(gs, func(i, j int) bool {
		// running guests first, always
		if gs[i].Running != gs[j].Running {
			return gs[i].Running
		}
		if reverse {
			return less(gs[j], gs[i])
		}
		return less(gs[i], gs[j])
	})
}

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/instantnodeeu/lxcpeek/internal/pve"
)

var version = "dev"

func main() {
	interval := flag.Duration("d", 2*time.Second, "refresh interval, with -once how long to sample")
	once := flag.Bool("once", false, "print a report and exit")
	hotOnly := flag.Bool("hot", false, "with -once: only hot guests, exit status 2 if there are any")
	asJSON := flag.Bool("json", false, "with -once: print JSON")
	showVersion := flag.Bool("v", false, "print version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "lxcpeek %s, top for Proxmox VE guests\n\nusage: lxcpeek [flags]\n\n", version)
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println("lxcpeek", version)
		return
	}
	// ponytail: one prefix for all roots, enough to point it at a copied tree
	if r := os.Getenv("LXCPEEK_ROOT"); r != "" {
		pve.EtcPVE, pve.Cgroup, pve.SysNet, pve.Proc, pve.Run = r+pve.EtcPVE, r+pve.Cgroup, r+pve.SysNet, r+pve.Proc, r+pve.Run
	}
	if _, err := os.Stat(pve.EtcPVE); err != nil {
		fmt.Fprintln(os.Stderr, "lxcpeek: /etc/pve not found, run this on a Proxmox VE node")
		os.Exit(1)
	}
	if *interval < 500*time.Millisecond {
		*interval = 500 * time.Millisecond
	}

	if *once {
		hot, err := printOnce(*interval, *asJSON, *hotOnly)
		if err != nil {
			fmt.Fprintln(os.Stderr, "lxcpeek:", err)
			if errors.Is(err, fs.ErrPermission) {
				fmt.Fprintln(os.Stderr, "lxcpeek: run it as root")
			}
			os.Exit(1)
		}
		if *hotOnly && hot > 0 {
			os.Exit(2)
		}
		return
	}
	if err := newApp(*interval).start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// printOnce samples for d, prints the report and returns the number of
// hot guests.
func printOnce(d time.Duration, asJSON, hotOnly bool) (int, error) {
	var c collector
	if _, err := c.collect(); err != nil {
		return 0, err
	}
	time.Sleep(d)
	snap, err := c.collect()
	if err != nil {
		return 0, err
	}
	sortGuests(snap.Guests, byCPU, false)

	hot := 0
	details := map[int]connSummary{}
	for _, g := range snap.Guests {
		if len(g.Hot) == 0 {
			continue
		}
		hot++
		// ponytail: one conntrack pass per hot guest, fine while hot is rare
		if len(g.IPs) > 0 && hot <= 10 && snap.CTErr == "" {
			if s, err := summarize(g.IPs); err == nil {
				details[g.ID] = s
			}
		}
	}

	if asJSON {
		if hotOnly {
			var hs []guestStat
			for _, g := range snap.Guests {
				if len(g.Hot) > 0 {
					hs = append(hs, g)
				}
			}
			snap.Guests = hs
		}
		e := json.NewEncoder(os.Stdout)
		e.SetIndent("", "  ")
		return hot, e.Encode(snap)
	}
	report(os.Stdout, snap, d, newANSI(colorOK()), hotOnly, details)
	return hot, nil
}

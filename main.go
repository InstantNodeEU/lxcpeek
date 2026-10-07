package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"text/tabwriter"
	"time"

	"github.com/instantnodeeu/lxcpeek/internal/pve"
)

var version = "dev"

func main() {
	interval := flag.Duration("d", 2*time.Second, "refresh interval")
	once := flag.Bool("once", false, "print one table and exit (sampled over -d)")
	asJSON := flag.Bool("json", false, "with -once: print JSON")
	showVersion := flag.Bool("v", false, "print version and exit")
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
		if err := printOnce(*interval, *asJSON); err != nil {
			fmt.Fprintln(os.Stderr, "lxcpeek:", err)
			if errors.Is(err, fs.ErrPermission) {
				fmt.Fprintln(os.Stderr, "lxcpeek: run it as root")
			}
			os.Exit(1)
		}
		return
	}
	if err := newApp(*interval).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func printOnce(d time.Duration, asJSON bool) error {
	var c collector
	if _, err := c.collect(); err != nil {
		return err
	}
	time.Sleep(d)
	snap, err := c.collect()
	if err != nil {
		return err
	}
	sortGuests(snap.Guests, byCPU, false)
	if asJSON {
		e := json.NewEncoder(os.Stdout)
		e.SetIndent("", "  ")
		return e.Encode(snap)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for i, h := range headers {
		if i > 0 {
			fmt.Fprint(w, "\t")
		}
		fmt.Fprint(w, h)
	}
	fmt.Fprintln(w)
	for _, g := range snap.Guests {
		for i, c := range cells(g) {
			if i > 0 {
				fmt.Fprint(w, "\t")
			}
			fmt.Fprint(w, c)
		}
		fmt.Fprintln(w)
	}
	if snap.CTErr != "" {
		fmt.Fprintln(w, "\nconnections:", snap.CTErr, "(run as root)")
	}
	return w.Flush()
}

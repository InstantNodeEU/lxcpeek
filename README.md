# <img src="assets/logo.svg" width="28" alt=""> lxcpeek

[![release](https://img.shields.io/github/v/release/instantnodeeu/lxcpeek?style=flat-square&color=f0883e)](https://github.com/instantnodeeu/lxcpeek/releases)
[![build](https://img.shields.io/github/actions/workflow/status/instantnodeeu/lxcpeek/test.yml?style=flat-square&label=build)](https://github.com/instantnodeeu/lxcpeek/actions)
[![license](https://img.shields.io/badge/license-MIT-blue?style=flat-square)](LICENSE)

top for the guests on a Proxmox VE node. Every container and VM in one
table, with CPU, memory, bandwidth, packets/s, disk IO and open connections.
The ones that look wrong are red.

![lxcpeek](assets/tui.png)

We host a lot of customer guests and the question at 3am is always the
same: which one is it? The miner pinning 16 cores, the box scanning port 23
across the internet, the one filling the conntrack table. The PVE web UI
can tell you, one guest at a time. lxcpeek tells you in one screen.

| Column | |
|---|---|
| CPU | percent of the cores the guest is allowed to use, so a 2 core container at 100% stands out on a 64 core host |
| MEM | working set against the limit |
| OUT, IN | bit/s, seen from the guest |
| PPS | packets per second the guest sends |
| IO/s | disk read + write per second |
| CONN | outgoing / incoming entries in the conntrack table |
| HOT | why the guest is marked |

A guest is marked hot when CPU (or memory, containers only) is at 90% of
its allotment, it sends more than 20k packets/s or has more than 1000
outgoing connections. VM memory is never flagged: QEMU keeps every page the
guest ever touched, so from the host almost every VM looks full.

Enter on a guest shows where it connects to, by port and by destination,
and says so when it looks like a port scan, outgoing spam or a mining pool.

![connections of one guest](assets/conns.png)

## Report

`lxcpeek -once` samples for two seconds and prints a report instead of the
TUI: the node, the hot guests with the reason and where they connect to,
then every guest.

![lxcpeek -once](assets/report.png)

Colors go away when the output is not a terminal or `NO_COLOR` is set, so
it pastes cleanly into a ticket or an abuse report.

`-hot` only prints the node and the hot guests and exits with status 2 if
there are any. That is enough for a cron job:

```sh
*/5 * * * * /usr/local/bin/lxcpeek -once -hot > /tmp/hot.txt || mail -s "hot guests on $(hostname)" ops@example.com < /tmp/hot.txt
```

`-json` gives the same data for scripts:

```sh
lxcpeek -once -json | jq -r '.guests[] | select(.hot) | "\(.id) \(.name) \(.hot)"'
```

## How it works

No agent in the guests, no API token, no config. It reads what the host
already has:

| | |
|---|---|
| guests, names, limits, IPs | `/etc/pve/lxc/*.conf`, `/etc/pve/qemu-server/*.conf`, ipfilter ipsets in `/etc/pve/firewall/<id>.fw` |
| CPU, memory, IO | cgroup v2, `/sys/fs/cgroup/lxc/<id>` and `qemu.slice/<id>.scope`; VM disk IO from `/proc/<qemu pid>/io` |
| network | host side ports `veth<id>i<n>` and `tap<id>i<n>` |
| connections | `/proc/net/nf_conntrack`, or `conntrack -L` if the kernel has no procfs file |

Connections are matched by guest IP, so a VM without cloud-init needs its
IP in an ipfilter ipset (which you want anyway against IP spoofing).
Connection counts only show traffic that passes conntrack, which on PVE means
the firewall has to be enabled for the guest.

The scan/spam/mining guesses are simple: more than 100 different hosts on
one port that is not web or DNS looks like a scan, more than 50 mail servers
looks like spam, and a few well known stratum ports look like a pool. They
are hints for where to look, not proof.

## Install

On the PVE node:

```sh
curl -fsSL https://raw.githubusercontent.com/instantnodeeu/lxcpeek/main/install.sh | sh
```

As root this puts the latest release in `/usr/local/bin` after checking its sha256.
Run the same line again to update, `VERSION=v0.2.0` pins a release, `BINDIR`
picks another directory. The binaries are also on the
[releases](https://github.com/instantnodeeu/lxcpeek/releases) page.

Or with Go 1.24+:

```sh
go install github.com/instantnodeeu/lxcpeek@latest
```

## Usage

```sh
lxcpeek                 # TUI, refresh every 2s
lxcpeek -d 5s           # slower, nicer on a full conntrack table
lxcpeek -once           # report
lxcpeek -once -hot      # only hot guests, exit status 2 if there are any
lxcpeek -once -json     # for scripts and alerting
```

Run it as root. `/etc/pve` and the conntrack table are not readable
otherwise.

## Keys

| Key | |
|---|---|
| `c` `m` `n` `k` `o` `i` | sort by CPU, memory, network, connections, disk IO, ID (again to reverse) |
| `/` | filter, `Esc` clears |
| `Enter` | connections of the selected guest |
| `q` | quit |

## Build

```sh
make          # ./lxcpeek
make test
make dist     # static linux amd64 + arm64 in dist/
```

The guests in the screenshots are made up and use documentation IP ranges.

## License

MIT. Written at [InstantNode](https://instantnode.eu), where it runs on our
own nodes.

# ECOmpeg2ts

ECOmpeg2ts is a userspace MPEG-2 Transport Stream analyzer for Linux routers,
set-top boxes, and small ARM boards. It watches UDP, multicast, or file input,
tracks MPEG-TS continuity counters per PID, and renders a live console dashboard
with packet, bitrate, drop, TEI, and discontinuity statistics.

The project was started for Amlogic S905x on Armbian Linux
`6.18.37-ophub`, but it does not depend on a kernel module or a specific CPU
architecture.

## Features

- UDP, multicast, and file input.
- MPEG-TS sync-byte resynchronization.
- Per-PID continuity counter tracking.
- Drop, duplicate, TEI, discontinuity, scrambled, and payload/adaptation stats.
- Colored ANSI console dashboard with per-PID bitrate and drop-rate columns.
- Sortable PID table: `--sort drops|bitrate|pid`.
- `--no-clear` mode for SSH/tmux logging.
- JSON-lines mode for machine-readable output and integrations.
- Static Linux ARM64 and AMD64 build targets.

## Build

```sh
make test
make build
make linux-arm64
make linux-amd64
```

Binaries are written to `dist/`.

### eBPF / TC Mode (Linux only)

```sh
make ebpf-object          # standard: 8 TS packets per UDP (1500 MTU)
make ebpf-object-jumbo    # jumbo: 16 TS packets per UDP (jumbo frames)
make linux-arm64-tc       # build ecompeg2ts-tc for ARM64
```

## Install On Armbian ARM64

Download the `ecompeg2ts-linux-arm64` binary from the latest GitHub release,
then install it on the target box:

```sh
chmod +x ecompeg2ts-linux-arm64
sudo install -m 0755 ecompeg2ts-linux-arm64 /usr/local/bin/ecompeg2ts
ecompeg2ts --help
```

Optional systemd service example:

```sh
sudo cp docs/systemd/ecompeg2ts.service /etc/systemd/system/ecompeg2ts.service
sudo systemctl daemon-reload
sudo systemctl enable --now ecompeg2ts
```

Edit the multicast group, port, and interface in the service before enabling it.

## Examples

Analyze a UDP stream on all interfaces:

```sh
ecompeg2ts --udp :1234
```

Join a multicast group:

```sh
ecompeg2ts --multicast 239.10.10.10:1234 --iface eth0
```

Join a multicast group from an IPTV-style URL. The interface is optional; when
it is omitted, the OS chooses the multicast interface:

```sh
ecompeg2ts --source udp://@239.3.1.1:1234
ecompeg2ts udp://@239.3.1.1:1234
```

Read a transport stream file:

```sh
ecompeg2ts --file sample.ts
```

Emit JSON lines instead of dashboard:

```sh
ecompeg2ts --udp :1234 --json
```

Dashboard with sorting and SSH-friendly output:

```sh
ecompeg2ts --multicast 239.3.1.1:1234 --sort bitrate
ecompeg2ts --multicast 239.3.1.1:1234 --sort drops --no-clear
```

## TC/eBPF Mode

For many simultaneous streams on small ARM boards, `ecompeg2ts-tc` attaches a TC
ingress eBPF program and reads aggregated counters from BPF maps instead of
copying every stream into userspace. This reduces CPU usage by ~250x.

Attach via TCX (default on modern kernels), with automatic clsact/netlink
fallback for older kernels:

```sh
sudo ./ecompeg2ts-tc --iface eth0 --object dist/ecompeg2ts_tc_bpfel.o --join udp://@239.3.1.1:1234
```

Force clsact/netlink attach mode:

```sh
sudo ./ecompeg2ts-tc --iface eth0 --object dist/ecompeg2ts_tc_bpfel.o --clsact --join udp://@239.3.1.1:1234
```

Jumbo frame variant (16 TS packets per UDP datagram):

```sh
sudo ./ecompeg2ts-tc --iface eth0 --object dist/ecompeg2ts_tc_bpfel_jumbo.o --join udp://@239.3.1.1:1234
```

See [docs/ebpf/README.md](docs/ebpf/README.md) for technical details.

### BPF Special PIDs

| PID | Meaning |
|-----|---------|
| `0xfffe` | Oversized datagrams: `packets` = oversized UDP datagrams, `drops` = TS packets not parsed beyond the limit |
| `0xffff` | Sync byte losses: `sync_losses` counter increments when expected sync byte `0x47` is missing |

## Notes

Continuity counter drops are counted per PID only for packets carrying payload.
Adaptation-only packets do not advance the expected counter. Packets with the
adaptation-field discontinuity indicator reset the PID expectation and are
counted separately.

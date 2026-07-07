# ECOmpeg2ts

ECOmpeg2ts is a userspace MPEG-2 Transport Stream analyzer for Linux routers,
set-top boxes, and small ARM boards. It watches UDP, multicast, or file input,
tracks MPEG-TS continuity counters per PID, and renders a live console dashboard
with packet, bitrate, drop, TEI, and discontinuity statistics.

The project was started for Amlogic S905x on Armbian Linux
`6.18.37-ophub`, but it does not depend on a kernel module or a specific CPU
architecture.

## Features

- UDP and multicast input.
- File input for captures and regression tests.
- MPEG-TS sync-byte resynchronization.
- Per-PID continuity counter tracking.
- Drop, duplicate, TEI, discontinuity, scrambled, and payload/adaptation stats.
- ANSI console dashboard with bitrate and drop history graphs.
- JSON-lines mode for logging and integrations.
- Static Linux ARM64 build target.

## Build

```sh
make test
make build
make linux-arm64
```

The ARM64 binary will be written to `dist/ecompeg2ts-linux-arm64`.

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

## Notes

Continuity counter drops are counted per PID only for packets carrying payload.
Adaptation-only packets do not advance the expected counter. Packets with the
adaptation-field discontinuity indicator reset the PID expectation and are
counted separately.

## Experimental TC/eBPF Mode

For many simultaneous streams on small ARM boards, the experimental
`ecompeg2ts-tc` command can attach a TC ingress eBPF program and read aggregated
counters from BPF maps instead of copying every stream into userspace.

See [docs/ebpf/README.md](docs/ebpf/README.md).

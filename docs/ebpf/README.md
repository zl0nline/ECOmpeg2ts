# Experimental TC/eBPF Mode

`ecompeg2ts-tc` is an experimental low-overhead datapath for multi-stream IPTV
monitoring. A TC ingress eBPF program parses IPv4/UDP MPEG-TS packets in the
kernel and stores per destination multicast group, UDP port, and PID counters in
a BPF map. Userspace only reads aggregated counters and renders the dashboard.

This is intended for cases where the pure userspace analyzer becomes too costly
on small ARM boards while monitoring many simultaneous streams.

## Build The BPF Object

The BPF object must be built on Linux with a clang that supports the BPF target:

```sh
clang -O2 -g -target bpf -I/usr/include/$(uname -m)-linux-gnu -c bpf/ecompeg2ts_tc.c -o ecompeg2ts_tc_bpfel.o
```

## Build The Loader

```sh
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o ecompeg2ts-tc ./cmd/ecompeg2ts-tc
```

## Run

The command requires root or the relevant BPF/network capabilities:

```sh
sudo ./ecompeg2ts-tc --iface eth0 --object ./ecompeg2ts_tc_bpfel.o
```

If no other process has joined the multicast group, the NIC may not receive the
stream. `ecompeg2ts-tc` can keep a dummy IGMP membership open:

```sh
sudo ./ecompeg2ts-tc --iface eth0 --object ./ecompeg2ts_tc_bpfel.o --join udp://@239.3.1.1:1234
```

Repeat `--join` for multiple multicast groups. The dummy socket is only used to
keep membership alive; MPEG-TS counters still come from the TC/eBPF map.

The first version uses TCX attach, which is available on recent kernels. The
target Armbian `6.18.37-ophub` kernel should be new enough. If TCX attach fails,
the fallback will be a later traditional `clsact`/netlink attach path.

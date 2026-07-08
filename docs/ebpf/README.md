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
make ebpf-object
```

The standard object parses up to 8 TS packets per UDP datagram, which covers
ordinary IPTV over 1500 MTU. To build a jumbo variant with 16 TS packets per UDP
datagram:

```sh
make ebpf-object-jumbo
```

## Build The Loader

```sh
make linux-arm64-tc
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

By default the loader tries TCX ingress first. If TCX is unavailable, it falls
back to traditional `clsact` qdisc + netlink SchedCLS attach without shelling
out to `tc`. Use `--clsact` to force the fallback path:

```sh
sudo ./ecompeg2ts-tc --iface eth0 --object ./ecompeg2ts_tc_bpfel.o --clsact
```

The dashboard reports the attach mode as either `tcx` or `clsact`.

## Oversized Datagrams

The BPF parser is compiled with a fixed `MAX_TS_PACKETS_PER_UDP` so the verifier
can prove the loop. Datagrams larger than that limit are capped for parsing and
reported under reserved PID `0xfffe`:

- `packets`: oversized UDP datagrams seen.
- `drops`: TS packets left unparsed beyond the compiled limit.

Use the standard object for normal IPTV streams and the jumbo object only when
the target kernel/verifier accepts it and the network really carries larger UDP
payloads.

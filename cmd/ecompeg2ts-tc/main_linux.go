//go:build linux

package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/zl0nline/ECOmpeg2ts/internal/input"
)

type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ",")
}

func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

type objects struct {
	Program *ebpf.Program `ebpf:"tc_mpeg2ts"`
	PIDMap  *ebpf.Map     `ebpf:"pid_stats"`
}

func (o *objects) Close() {
	if o.Program != nil {
		o.Program.Close()
	}
	if o.PIDMap != nil {
		o.PIDMap.Close()
	}
}

type streamPIDKey struct {
	DstIP   uint32
	DstPort uint16
	PID     uint16
}

type pidStatsValue struct {
	Packets         uint64
	Drops           uint64
	Duplicates      uint64
	TEIErrors       uint64
	Discontinuities uint64
	SyncLosses      uint64
	LastCC          uint8
	Seen            uint8
	Reserved0       uint8
	Reserved1       uint8
	Pad             [4]byte
}

type row struct {
	Key   streamPIDKey
	Value pidStatsValue
}

func main() {
	ifaceName := flag.String("iface", "", "interface to attach TC ingress program to, for example eth0")
	objectPath := flag.String("object", "ecompeg2ts_tc_bpfel.o", "compiled eBPF object path")
	interval := flag.Duration("interval", time.Second, "dashboard refresh interval")
	var joins stringList
	flag.Var(&joins, "join", "multicast source URL to join for IGMP, repeatable, for example udp://@239.3.1.1:1234")
	flag.Parse()

	if *ifaceName == "" {
		fmt.Fprintln(os.Stderr, "--iface is required for TC/eBPF mode")
		os.Exit(2)
	}

	iface, err := net.InterfaceByName(*ifaceName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	joinConns, err := openJoins(joins, iface)
	if err != nil {
		fmt.Fprintf(os.Stderr, "join multicast: %v\n", err)
		os.Exit(2)
	}
	defer closeJoins(joinConns)

	spec, err := ebpf.LoadCollectionSpec(*objectPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load BPF object: %v\n", err)
		os.Exit(1)
	}

	var objs objects
	if err := spec.LoadAndAssign(&objs, nil); err != nil {
		fmt.Fprintf(os.Stderr, "load BPF collection: %v\n", err)
		os.Exit(1)
	}
	defer objs.Close()

	tc, err := link.AttachTCX(link.TCXOptions{
		Interface: iface.Index,
		Program:   objs.Program,
		Attach:    ebpf.AttachTCXIngress,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "attach TCX ingress: %v\n", err)
		os.Exit(1)
	}
	defer tc.Close()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			render(objs.PIDMap, *ifaceName)
		case <-sigCh:
			fmt.Fprintln(os.Stderr, "detaching")
			return
		}
	}
}

func render(m *ebpf.Map, iface string) {
	rows := readRows(m)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Value.Drops == rows[j].Value.Drops {
			return rows[i].Value.Packets > rows[j].Value.Packets
		}
		return rows[i].Value.Drops > rows[j].Value.Drops
	})

	fmt.Print("\033[2J\033[H")
	fmt.Printf("ECOmpeg2ts TC/eBPF  iface=%s  streams/pids=%d\n", iface, len(rows))
	fmt.Println("dst              port   pid     packets      drops  dup    tei   disc  sync")
	fmt.Println("----------------------------------------------------------------------------")
	limit := 24
	if len(rows) < limit {
		limit = len(rows)
	}
	for i := 0; i < limit; i++ {
		r := rows[i]
		fmt.Printf("%-15s  %-5d  0x%04x  %10d  %7d  %5d  %5d  %5d  %4d\n",
			ipv4FromKey(r.Key.DstIP),
			ntohs(r.Key.DstPort),
			r.Key.PID,
			r.Value.Packets,
			r.Value.Drops,
			r.Value.Duplicates,
			r.Value.TEIErrors,
			r.Value.Discontinuities,
			r.Value.SyncLosses,
		)
	}
}

func readRows(m *ebpf.Map) []row {
	rows := make([]row, 0)
	var key streamPIDKey
	var value pidStatsValue
	iter := m.Iterate()
	for iter.Next(&key, &value) {
		rows = append(rows, row{Key: key, Value: value})
	}
	return rows
}

func ipv4FromKey(v uint32) net.IP {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	return net.IPv4(b[0], b[1], b[2], b[3])
}

func ntohs(v uint16) uint16 {
	return (v << 8) | (v >> 8)
}

func openJoins(rawSources []string, iface *net.Interface) ([]*net.UDPConn, error) {
	conns := make([]*net.UDPConn, 0, len(rawSources))
	for _, raw := range rawSources {
		spec, err := input.ParseSource(raw)
		if err != nil {
			closeJoins(conns)
			return nil, err
		}
		if spec.Scheme != "udp" || !spec.IsMulticast {
			closeJoins(conns)
			return nil, fmt.Errorf("--join expects a multicast udp:// source, got %q", raw)
		}
		addr, err := net.ResolveUDPAddr("udp", spec.Address)
		if err != nil {
			closeJoins(conns)
			return nil, err
		}
		conn, err := net.ListenMulticastUDP("udp", iface, addr)
		if err != nil {
			closeJoins(conns)
			return nil, err
		}
		_ = conn.SetReadBuffer(64 * 1024)
		conns = append(conns, conn)
	}
	return conns, nil
}

func closeJoins(conns []*net.UDPConn) {
	for _, conn := range conns {
		_ = conn.Close()
	}
}

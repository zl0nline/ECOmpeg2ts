//go:build linux

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/vishvananda/netlink"
	"github.com/zl0nline/ECOmpeg2ts/internal/dashboard"
	"github.com/zl0nline/ECOmpeg2ts/internal/input"
	"github.com/zl0nline/ECOmpeg2ts/internal/mpegts"
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
	AdaptationOnly  uint64
	PayloadPackets  uint64
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

// attachMode describes how the BPF program was attached so we can log it
// and clean up properly on exit.
type attachMode int

const (
	attachTCX attachMode = iota
	attachClsact
)

func (m attachMode) String() string {
	switch m {
	case attachTCX:
		return "tcx"
	case attachClsact:
		return "clsact"
	default:
		return "unknown"
	}
}

func main() {
	ifaceName := flag.String("iface", "", "interface to attach TC ingress program to, for example eth0")
	objectPath := flag.String("object", "ecompeg2ts_tc_bpfel.o", "compiled eBPF object path")
	interval := flag.Duration("interval", time.Second, "dashboard refresh interval")
	jsonMode := flag.Bool("json", false, "emit JSON snapshots instead of the dashboard")
	noClear := flag.Bool("no-clear", false, "do not clear screen each tick (for SSH/tmux logging)")
	sortMode := flag.String("sort", "drops", "PID sort mode: drops, bitrate, pid")
	var joins stringList
	flag.Var(&joins, "join", "multicast source URL to join for IGMP, repeatable, for example udp://@239.3.1.1:1234")
	preferClsact := flag.Bool("clsact", false, "prefer clsact/netlink attach even if TCX is available")
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

	// Attach BPF program: TCX (default) or clsact/netlink fallback.
	mode, cleanup, err := attachBPF(iface, objs.Program, *preferClsact)
	if err != nil {
		fmt.Fprintf(os.Stderr, "attach failed: %v\n", err)
		os.Exit(1)
	}
	defer cleanup()

	switch mode {
	case attachTCX:
		fmt.Fprintf(os.Stderr, "attached via TCX ingress on %s\n", iface.Name)
	case attachClsact:
		fmt.Fprintf(os.Stderr, "attached via clsact/netlink ingress on %s (fallback)\n", iface.Name)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	startedAt := time.Now()
	sourceName := fmt.Sprintf("tc/eBPF iface=%s attach=%s", iface.Name, mode.String())
	renderer := dashboard.New(os.Stdout, sourceName)
	renderer.SetNoClear(*noClear)
	switch *sortMode {
	case "bitrate":
		renderer.SetSortMode(dashboard.SortByBitrate)
	case "pid":
		renderer.SetSortMode(dashboard.SortByPID)
	}
	encoder := json.NewEncoder(os.Stdout)

	for {
		select {
		case <-ticker.C:
			s := snapshotFromMap(objs.PIDMap, startedAt)
			if *jsonMode {
				if err := encoder.Encode(s); err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
			} else {
				renderer.Render(s)
			}
		case <-sigCh:
			fmt.Fprintln(os.Stderr, "detaching")
			return
		}
	}
}

// attachBPF tries TCX first (unless --clsact is set), then falls back to
// clsact qdisc + netlink SchedCLS attach. Returns the attach mode, a cleanup
// function, and any error.
func attachBPF(iface *net.Interface, prog *ebpf.Program, preferClsact bool) (attachMode, func(), error) {
	if !preferClsact {
		tc, err := link.AttachTCX(link.TCXOptions{
			Interface: iface.Index,
			Program:   prog,
			Attach:    ebpf.AttachTCXIngress,
		})
		if err == nil {
			return attachTCX, func() { tc.Close() }, nil
		}
		fmt.Fprintf(os.Stderr, "TCX attach failed (%v), trying clsact/netlink fallback...\n", err)
	}

	// clsact/netlink fallback
	nlLink, err := netlink.LinkByName(iface.Name)
	if err != nil {
		return 0, nil, fmt.Errorf("netlink: find interface %s: %w", iface.Name, err)
	}

	// Ensure a clsact qdisc exists. clsact is idempotent: adding when one
	// already exists returns EEXIST, which we ignore.
	clsact := &netlink.Clsact{QdiscAttrs: netlink.QdiscAttrs{
		LinkIndex: nlLink.Attrs().Index,
		Handle:    netlink.MakeHandle(0xffff, 0),
		Parent:    netlink.HANDLE_CLSACT,
	}}
	if err := netlink.QdiscAdd(clsact); err != nil && !isExistErr(err) {
		return 0, nil, fmt.Errorf("netlink: add clsact qdisc: %w", err)
	}

	// Attach the SchedCLS filter on clsact ingress.
	// Ingress parent = (TC_H_CLSACT | TC_H_MIN_INGRESS) = HANDLE_MIN_INGRESS.
	filter := &netlink.BpfFilter{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: nlLink.Attrs().Index,
			Parent:    netlink.HANDLE_MIN_INGRESS,
			Handle:    netlink.MakeHandle(0, 1),
			Protocol:  syscall.ETH_P_ALL,
			Priority:  1,
		},
		Fd:           prog.FD(),
		DirectAction: true,
	}

	if err := netlink.FilterAdd(filter); err != nil && !isExistErr(err) {
		return 0, nil, fmt.Errorf("netlink: add SchedCLS filter: %w", err)
	}

	cleanup := func() {
		// Filter removal is best-effort; qdisc removal auto-cleans filters.
		_ = netlink.FilterDel(filter)
	}

	return attachClsact, cleanup, nil
}

// isExistErr returns true if the error indicates the object already exists.
func isExistErr(err error) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), "exists") || contains(err.Error(), "EEXIST") || errnoIsExist(err)
}

func errnoIsExist(err error) bool {
	type errnof interface{ Errno() syscall.Errno }
	if e, ok := err.(errnof); ok {
		return e.Errno() == syscall.EEXIST
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func snapshotFromMap(m *ebpf.Map, startedAt time.Time) mpegts.Snapshot {
	rows := readRows(m)
	byPID := make(map[uint16]*mpegts.PIDStats, len(rows))
	s := mpegts.Snapshot{
		StartedAt: startedAt,
		TakenAt:   time.Now(),
	}

	for _, r := range rows {
		p := byPID[r.Key.PID]
		if p == nil {
			p = &mpegts.PIDStats{PID: r.Key.PID}
			byPID[r.Key.PID] = p
		}

		bytes := r.Value.Packets * mpegts.PacketSize
		p.Packets += r.Value.Packets
		p.Bytes += bytes
		p.Drops += r.Value.Drops
		p.Duplicates += r.Value.Duplicates
		p.TEIErrors += r.Value.TEIErrors
		p.Discontinuities += r.Value.Discontinuities
		p.AdaptationOnly += r.Value.AdaptationOnly
		p.PayloadPackets += r.Value.PayloadPackets

		s.Packets += r.Value.Packets
		s.Bytes += bytes
		s.Drops += r.Value.Drops
		s.Duplicates += r.Value.Duplicates
		s.TEIErrors += r.Value.TEIErrors
		s.Discontinuities += r.Value.Discontinuities
		s.SyncLosses += r.Value.SyncLosses
	}

	s.PIDs = make([]mpegts.PIDStats, 0, len(byPID))
	for _, p := range byPID {
		s.PIDs = append(s.PIDs, *p)
	}
	return s
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

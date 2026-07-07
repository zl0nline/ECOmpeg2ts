package mpegts

import (
	"sort"
	"sync"
	"time"
)

const PacketSize = 188
const SyncByte = 0x47

type PIDStats struct {
	PID             uint16 `json:"pid"`
	Packets         uint64 `json:"packets"`
	Bytes           uint64 `json:"bytes"`
	Drops           uint64 `json:"drops"`
	Duplicates      uint64 `json:"duplicates"`
	TEIErrors       uint64 `json:"tei_errors"`
	Discontinuities uint64 `json:"discontinuities"`
	Scrambled       uint64 `json:"scrambled"`
	AdaptationOnly  uint64 `json:"adaptation_only"`
	PayloadPackets  uint64 `json:"payload_packets"`
	ReservedAFC     uint64 `json:"reserved_afc"`

	lastCC uint8
	seen   bool
}

type Snapshot struct {
	StartedAt       time.Time  `json:"started_at"`
	TakenAt         time.Time  `json:"taken_at"`
	Packets         uint64     `json:"packets"`
	Bytes           uint64     `json:"bytes"`
	SyncLosses      uint64     `json:"sync_losses"`
	Drops           uint64     `json:"drops"`
	Duplicates      uint64     `json:"duplicates"`
	TEIErrors       uint64     `json:"tei_errors"`
	Discontinuities uint64     `json:"discontinuities"`
	Scrambled       uint64     `json:"scrambled"`
	ReservedAFC     uint64     `json:"reserved_afc"`
	PIDs            []PIDStats `json:"pids"`
}

type Analyzer struct {
	mu              sync.Mutex
	startedAt       time.Time
	buf             []byte
	pids            map[uint16]*PIDStats
	packets         uint64
	bytes           uint64
	syncLosses      uint64
	drops           uint64
	duplicates      uint64
	teiErrors       uint64
	discontinuities uint64
	scrambled       uint64
	reservedAFC     uint64
}

func NewAnalyzer() *Analyzer {
	return &Analyzer{
		startedAt: time.Now(),
		pids:      make(map[uint16]*PIDStats),
	}
}

func (a *Analyzer) Feed(data []byte) {
	if len(data) == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.buf = append(a.buf, data...)
	for {
		if len(a.buf) < PacketSize {
			return
		}
		if a.buf[0] != SyncByte {
			idx := findSync(a.buf)
			if idx < 0 {
				a.syncLosses += uint64(len(a.buf))
				a.buf = a.buf[:0]
				return
			}
			a.syncLosses += uint64(idx)
			a.buf = a.buf[idx:]
			if len(a.buf) < PacketSize {
				return
			}
		}
		a.parsePacket(a.buf[:PacketSize])
		copy(a.buf, a.buf[PacketSize:])
		a.buf = a.buf[:len(a.buf)-PacketSize]
	}
}

func (a *Analyzer) Snapshot() Snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	pids := make([]PIDStats, 0, len(a.pids))
	for _, stat := range a.pids {
		copyStat := *stat
		copyStat.lastCC = 0
		copyStat.seen = false
		pids = append(pids, copyStat)
	}
	sort.Slice(pids, func(i, j int) bool {
		if pids[i].Drops == pids[j].Drops {
			return pids[i].Packets > pids[j].Packets
		}
		return pids[i].Drops > pids[j].Drops
	})
	return Snapshot{
		StartedAt:       a.startedAt,
		TakenAt:         time.Now(),
		Packets:         a.packets,
		Bytes:           a.bytes,
		SyncLosses:      a.syncLosses,
		Drops:           a.drops,
		Duplicates:      a.duplicates,
		TEIErrors:       a.teiErrors,
		Discontinuities: a.discontinuities,
		Scrambled:       a.scrambled,
		ReservedAFC:     a.reservedAFC,
		PIDs:            pids,
	}
}

func (a *Analyzer) parsePacket(pkt []byte) {
	tei := pkt[1]&0x80 != 0
	pid := (uint16(pkt[1]&0x1f) << 8) | uint16(pkt[2])
	afc := (pkt[3] >> 4) & 0x03
	cc := pkt[3] & 0x0f
	scrambled := pkt[3]&0xc0 != 0
	hasAdaptation := afc == 2 || afc == 3
	hasPayload := afc == 1 || afc == 3
	discontinuity := false

	if hasAdaptation && len(pkt) > 5 {
		afLen := int(pkt[4])
		if afLen > 0 && 5+afLen <= len(pkt) {
			discontinuity = pkt[5]&0x80 != 0
		}
	}

	stat := a.pids[pid]
	if stat == nil {
		stat = &PIDStats{PID: pid}
		a.pids[pid] = stat
	}

	a.packets++
	a.bytes += PacketSize
	stat.Packets++
	stat.Bytes += PacketSize

	if tei {
		a.teiErrors++
		stat.TEIErrors++
	}
	if scrambled {
		a.scrambled++
		stat.Scrambled++
	}
	if afc == 2 {
		stat.AdaptationOnly++
	}
	if hasPayload {
		stat.PayloadPackets++
	}
	if afc == 0 {
		a.reservedAFC++
		stat.ReservedAFC++
		return
	}
	if discontinuity {
		a.discontinuities++
		stat.Discontinuities++
		stat.seen = false // reset: next payload packet establishes new CC baseline
		return
	}
	if !hasPayload {
		return
	}
	if !stat.seen {
		stat.lastCC = cc
		stat.seen = true
		return
	}
	expected := (stat.lastCC + 1) & 0x0f
	switch {
	case cc == expected:
		stat.lastCC = cc
	case cc == stat.lastCC:
		a.duplicates++
		stat.Duplicates++
	default:
		missing := (cc - expected) & 0x0f
		if missing == 0 {
			missing = 1
		}
		a.drops += uint64(missing)
		stat.Drops += uint64(missing)
		stat.lastCC = cc
	}
}

func findSync(buf []byte) int {
	for i, b := range buf {
		if b == SyncByte {
			return i
		}
	}
	return -1
}

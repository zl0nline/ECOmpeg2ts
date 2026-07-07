package mpegts

import "testing"

// --- helpers ---

func packet(pid uint16, cc uint8, payload bool, discontinuity bool) []byte {
	p := make([]byte, PacketSize)
	p[0] = SyncByte
	p[1] = byte(pid >> 8)
	p[2] = byte(pid)
	if payload && discontinuity {
		p[3] = 0x30 | (cc & 0x0f) // afc=3 (adaptation+payload)
		p[4] = 1                  // adaptation field length
		p[5] = 0x80               // discontinuity_indicator
		return p
	}
	if payload {
		p[3] = 0x10 | (cc & 0x0f) // afc=1 (payload only)
		return p
	}
	// adaptation only
	p[3] = 0x20 | (cc & 0x0f) // afc=2 (adaptation only)
	p[4] = 1
	if discontinuity {
		p[5] = 0x80
	}
	return p
}

func packetWithAFC(pid uint16, cc uint8, afc uint8) []byte {
	p := make([]byte, PacketSize)
	p[0] = SyncByte
	p[1] = byte(pid >> 8)
	p[2] = byte(pid)
	p[3] = (afc << 4) | (cc & 0x0f)
	return p
}

func packetWithTEI(pid uint16, cc uint8) []byte {
	p := make([]byte, PacketSize)
	p[0] = SyncByte
	p[1] = 0x80 | byte(pid>>8) // TEI bit set
	p[2] = byte(pid)
	p[3] = 0x10 | (cc & 0x0f)
	return p
}

func packetScrambled(pid uint16, cc uint8) []byte {
	p := make([]byte, PacketSize)
	p[0] = SyncByte
	p[1] = byte(pid >> 8)
	p[2] = byte(pid)
	p[3] = 0x50 | (cc & 0x0f) // TSC=01 (scrambled), afc=1
	return p
}

// --- existing tests (kept) ---

func TestContinuityDrop(t *testing.T) {
	a := NewAnalyzer()
	a.Feed(packet(100, 0, true, false))
	a.Feed(packet(100, 1, true, false))
	a.Feed(packet(100, 4, true, false))

	s := a.Snapshot()
	if s.Drops != 2 {
		t.Fatalf("drops = %d, want 2", s.Drops)
	}
}

func TestAdaptationOnlyDoesNotAdvanceCounter(t *testing.T) {
	a := NewAnalyzer()
	a.Feed(packet(42, 7, true, false))
	a.Feed(packet(42, 7, false, false))
	a.Feed(packet(42, 8, true, false))

	s := a.Snapshot()
	if s.Drops != 0 {
		t.Fatalf("drops = %d, want 0", s.Drops)
	}
}

func TestDiscontinuityResetsExpectation(t *testing.T) {
	a := NewAnalyzer()
	a.Feed(packet(7, 1, true, false))
	a.Feed(packet(7, 8, true, true))
	a.Feed(packet(7, 9, true, false))

	s := a.Snapshot()
	if s.Drops != 0 {
		t.Fatalf("drops = %d, want 0", s.Drops)
	}
	if s.Discontinuities != 1 {
		t.Fatalf("discontinuities = %d, want 1", s.Discontinuities)
	}
}

// --- new tests ---

func TestCCWraparound(t *testing.T) {
	a := NewAnalyzer()
	a.Feed(packet(100, 14, true, false))
	a.Feed(packet(100, 15, true, false))
	a.Feed(packet(100, 0, true, false)) // wrap 15→0, normal

	s := a.Snapshot()
	if s.Drops != 0 {
		t.Fatalf("drops = %d, want 0 (normal wraparound)", s.Drops)
	}
}

func TestCCWraparoundWithDrop(t *testing.T) {
	a := NewAnalyzer()
	a.Feed(packet(100, 15, true, false))
	a.Feed(packet(100, 2, true, false)) // wrap: expected 0, got 2 → 2 drops

	s := a.Snapshot()
	if s.Drops != 2 {
		t.Fatalf("drops = %d, want 2", s.Drops)
	}
}

func TestCCWraparoundLargeGap(t *testing.T) {
	a := NewAnalyzer()
	a.Feed(packet(100, 14, true, false))
	a.Feed(packet(100, 3, true, false)) // expected 15, got 3 → (3-15)&0xf = 4 drops

	s := a.Snapshot()
	if s.Drops != 4 {
		t.Fatalf("drops = %d, want 4", s.Drops)
	}
}

func TestDuplicate(t *testing.T) {
	a := NewAnalyzer()
	a.Feed(packet(100, 5, true, false))
	a.Feed(packet(100, 5, true, false)) // duplicate CC

	s := a.Snapshot()
	if s.Duplicates != 1 {
		t.Fatalf("duplicates = %d, want 1", s.Duplicates)
	}
	if s.Drops != 0 {
		t.Fatalf("drops = %d, want 0", s.Drops)
	}
}

func TestTEIDetected(t *testing.T) {
	a := NewAnalyzer()
	a.Feed(packetWithTEI(100, 0))
	a.Feed(packetWithTEI(100, 1))

	s := a.Snapshot()
	if s.TEIErrors != 2 {
		t.Fatalf("tei_errors = %d, want 2", s.TEIErrors)
	}
}

func TestScrambledDetected(t *testing.T) {
	a := NewAnalyzer()
	a.Feed(packetScrambled(100, 0))
	a.Feed(packetScrambled(100, 1))

	s := a.Snapshot()
	if s.Scrambled != 2 {
		t.Fatalf("scrambled = %d, want 2", s.Scrambled)
	}
}

func TestMultiplePIDsInterleaved(t *testing.T) {
	a := NewAnalyzer()
	// PID 100: CC 0,1,2 (no drops)
	// PID 200: CC 0,1,3 (1 drop)
	a.Feed(packet(100, 0, true, false))
	a.Feed(packet(200, 0, true, false))
	a.Feed(packet(100, 1, true, false))
	a.Feed(packet(200, 1, true, false))
	a.Feed(packet(100, 2, true, false))
	a.Feed(packet(200, 3, true, false)) // expected 2, got 3 → 1 drop

	s := a.Snapshot()
	if s.Drops != 1 {
		t.Fatalf("drops = %d, want 1", s.Drops)
	}
	if len(s.PIDs) != 2 {
		t.Fatalf("pids count = %d, want 2", len(s.PIDs))
	}
	// PID 200 should have the drop
	for _, p := range s.PIDs {
		if p.PID == 200 && p.Drops != 1 {
			t.Fatalf("pid 200 drops = %d, want 1", p.Drops)
		}
	}
}

func TestSyncLossRecovery(t *testing.T) {
	a := NewAnalyzer()
	// garbage + valid packet
	garbage := []byte{0xFF, 0xEE, 0x00, 0x47}
	pkt := packet(100, 0, true, false)
	data := append(garbage, pkt...)

	a.Feed(data)

	s := a.Snapshot()
	if s.SyncLosses != 3 {
		t.Fatalf("sync_losses = %d, want 3", s.SyncLosses)
	}
	if s.Packets != 1 {
		t.Fatalf("packets = %d, want 1", s.Packets)
	}
}

func TestReservedAFC(t *testing.T) {
	a := NewAnalyzer()
	a.Feed(packetWithAFC(100, 0, 0)) // afc=0, reserved
	a.Feed(packetWithAFC(100, 1, 1)) // afc=1, normal payload

	s := a.Snapshot()
	if s.ReservedAFC != 1 {
		t.Fatalf("reserved_afc = %d, want 1", s.ReservedAFC)
	}
	if s.Packets != 2 {
		t.Fatalf("packets = %d, want 2", s.Packets)
	}
}

func TestAdaptationOnlyWithDiscontinuity(t *testing.T) {
	a := NewAnalyzer()
	a.Feed(packet(100, 5, true, false))    // payload, CC=5
	a.Feed(packet(100, 5, false, true))    // adaptation-only with discontinuity
	a.Feed(packet(100, 9, true, false))    // payload, CC=9 — should NOT trigger drop

	s := a.Snapshot()
	if s.Drops != 0 {
		t.Fatalf("drops = %d, want 0 (discontinuity resets expectation)", s.Drops)
	}
	if s.Discontinuities != 1 {
		t.Fatalf("discontinuities = %d, want 1", s.Discontinuities)
	}
}

func TestSequentialDrops(t *testing.T) {
	a := NewAnalyzer()
	a.Feed(packet(100, 0, true, false))
	a.Feed(packet(100, 5, true, false))  // 4 drops (expected 1)
	a.Feed(packet(100, 8, true, false))  // 2 drops (expected 6)

	s := a.Snapshot()
	if s.Drops != 6 {
		t.Fatalf("drops = %d, want 6", s.Drops)
	}
}

func TestLargeGapMaxDrops(t *testing.T) {
	a := NewAnalyzer()
	a.Feed(packet(100, 0, true, false))
	a.Feed(packet(100, 15, true, false)) // expected 1, got 15 → (15-1)&0xf = 14 drops

	s := a.Snapshot()
	if s.Drops != 14 {
		t.Fatalf("drops = %d, want 14 (maximum single-gap drop count)", s.Drops)
	}
}

func TestEmptyFeed(t *testing.T) {
	a := NewAnalyzer()
	a.Feed([]byte{})
	s := a.Snapshot()
	if s.Packets != 0 {
		t.Fatalf("packets = %d, want 0", s.Packets)
	}
}

func TestPartialPacketBuffered(t *testing.T) {
	a := NewAnalyzer()
	pkt := packet(100, 0, true, false)
	a.Feed(pkt[:100]) // partial
	s := a.Snapshot()
	if s.Packets != 0 {
		t.Fatalf("packets = %d, want 0 (partial should be buffered)", s.Packets)
	}
	// feed rest
	a.Feed(pkt[100:])
	s = a.Snapshot()
	if s.Packets != 1 {
		t.Fatalf("packets = %d, want 1 (completed packet)", s.Packets)
	}
}

func TestMultiplePacketsInOneFeed(t *testing.T) {
	a := NewAnalyzer()
	var data []byte
	data = append(data, packet(100, 0, true, false)...)
	data = append(data, packet(100, 1, true, false)...)
	data = append(data, packet(100, 2, true, false)...)
	a.Feed(data)
	s := a.Snapshot()
	if s.Packets != 3 {
		t.Fatalf("packets = %d, want 3", s.Packets)
	}
	if s.Drops != 0 {
		t.Fatalf("drops = %d, want 0", s.Drops)
	}
}

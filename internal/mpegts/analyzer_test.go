package mpegts

import "testing"

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

func packet(pid uint16, cc uint8, payload bool, discontinuity bool) []byte {
	p := make([]byte, PacketSize)
	p[0] = SyncByte
	p[1] = byte(pid >> 8)
	p[2] = byte(pid)
	if payload && discontinuity {
		p[3] = 0x30 | (cc & 0x0f)
		p[4] = 1
		p[5] = 0x80
		return p
	}
	if payload {
		p[3] = 0x10 | (cc & 0x0f)
		return p
	}
	p[3] = 0x20 | (cc & 0x0f)
	p[4] = 1
	if discontinuity {
		p[5] = 0x80
	}
	return p
}

package dashboard

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/zl0nline/ECOmpeg2ts/internal/mpegts"
)

type Renderer struct {
	out       io.Writer
	source    string
	last      mpegts.Snapshot
	lastTime  time.Time
	bitrates  []float64
	dropRates []float64
}

func New(out io.Writer, source string) *Renderer {
	return &Renderer{out: out, source: source}
}

func (r *Renderer) Render(s mpegts.Snapshot) {
	now := s.TakenAt
	elapsed := now.Sub(s.StartedAt).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}

	var mbps, pps, dropsPerSec float64
	if !r.lastTime.IsZero() {
		dt := now.Sub(r.lastTime).Seconds()
		if dt > 0 {
			mbps = float64(s.Bytes-r.last.Bytes) * 8 / dt / 1_000_000
			pps = float64(s.Packets-r.last.Packets) / dt
			dropsPerSec = float64(s.Drops-r.last.Drops) / dt
		}
	}
	r.bitrates = appendLimit(r.bitrates, mbps, 48)
	r.dropRates = appendLimit(r.dropRates, dropsPerSec, 48)
	r.last = s
	r.lastTime = now

	fmt.Fprint(r.out, "\033[2J\033[H")
	fmt.Fprintf(r.out, "ECOmpeg2ts  source=%s  uptime=%s\n", r.source, time.Duration(elapsed)*time.Second)
	fmt.Fprintln(r.out, strings.Repeat("=", 92))
	fmt.Fprintf(r.out, "packets=%d  bitrate=%.2f Mbps  pps=%.0f  drops=%d  dup=%d  tei=%d  disc=%d  sync_loss=%d\n",
		s.Packets, mbps, pps, s.Drops, s.Duplicates, s.TEIErrors, s.Discontinuities, s.SyncLosses)
	fmt.Fprintf(r.out, "bitrate  [%s] %.2f Mbps\n", spark(r.bitrates, 48), mbps)
	fmt.Fprintf(r.out, "drops/s  [%s] %.2f\n", spark(r.dropRates, 48), dropsPerSec)
	fmt.Fprintln(r.out)
	fmt.Fprintln(r.out, "Top PIDs by drops, then packet volume")
	fmt.Fprintln(r.out, "PID     packets      drops  dup    tei   disc  payload   adaptonly  bar")
	fmt.Fprintln(r.out, strings.Repeat("-", 92))

	pids := append([]mpegts.PIDStats(nil), s.PIDs...)
	sort.Slice(pids, func(i, j int) bool {
		if pids[i].Drops == pids[j].Drops {
			return pids[i].Packets > pids[j].Packets
		}
		return pids[i].Drops > pids[j].Drops
	})
	limit := 16
	if len(pids) < limit {
		limit = len(pids)
	}
	var maxPackets uint64 = 1
	for i := 0; i < limit; i++ {
		if pids[i].Packets > maxPackets {
			maxPackets = pids[i].Packets
		}
	}
	for i := 0; i < limit; i++ {
		p := pids[i]
		fmt.Fprintf(r.out, "0x%04x  %10d  %7d  %5d  %5d  %5d  %7d  %9d  %s\n",
			p.PID, p.Packets, p.Drops, p.Duplicates, p.TEIErrors, p.Discontinuities,
			p.PayloadPackets, p.AdaptationOnly, bar(p.Packets, maxPackets, 18))
	}
	fmt.Fprintln(r.out)
	fmt.Fprintln(r.out, "Ctrl+C to stop. Use --json for machine-readable output.")
}

func appendLimit(values []float64, value float64, limit int) []float64 {
	values = append(values, value)
	if len(values) > limit {
		return values[len(values)-limit:]
	}
	return values
}

func spark(values []float64, width int) string {
	if len(values) == 0 {
		return strings.Repeat(" ", width)
	}
	var max float64
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	if max <= 0 {
		return strings.Repeat(".", width)
	}
	var b strings.Builder
	start := 0
	if len(values) > width {
		start = len(values) - width
	}
	for _, v := range values[start:] {
		switch ratio := v / max; {
		case ratio > 0.80:
			b.WriteByte('#')
		case ratio > 0.55:
			b.WriteByte('=')
		case ratio > 0.25:
			b.WriteByte('-')
		case ratio > 0:
			b.WriteByte('.')
		default:
			b.WriteByte(' ')
		}
	}
	for b.Len() < width {
		b.WriteByte(' ')
	}
	return b.String()
}

func bar(value, max uint64, width int) string {
	if max == 0 {
		return strings.Repeat(" ", width)
	}
	n := int(float64(value) / float64(max) * float64(width))
	if n < 1 && value > 0 {
		n = 1
	}
	if n > width {
		n = width
	}
	return strings.Repeat("#", n) + strings.Repeat(" ", width-n)
}

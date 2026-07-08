package dashboard

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/zl0nline/ECOmpeg2ts/internal/mpegts"
	"golang.org/x/term"
)

// ANSI colour helpers.
const (
	colReset  = "\033[0m"
	colRed    = "\033[31m"
	colGreen  = "\033[32m"
	colYellow = "\033[33m"
	colBlue   = "\033[34m"
	colCyan   = "\033[36m"
	colBold   = "\033[1m"
	colDim    = "\033[2m"
)

type Renderer struct {
	out         io.Writer
	source      string
	noClear     bool
	sortMode    SortMode
	last        mpegts.Snapshot
	lastTime    time.Time
	bitrates    []float64
	dropRates   []float64
	termWidth   int
	pidBitrates map[uint16]float64 // computed each render
	pidDropRates map[uint16]float64
}

// SortMode controls PID table ordering.
type SortMode int

const (
	SortByDrops SortMode = iota
	SortByBitrate
	SortByPID
)

func New(out io.Writer, source string) *Renderer {
	return &Renderer{
		out:          out,
		source:       source,
		sortMode:     SortByDrops,
		pidBitrates:  make(map[uint16]float64),
		pidDropRates: make(map[uint16]float64),
	}
}

// SetNoClear disables screen clearing (for SSH/tmux logging).
func (r *Renderer) SetNoClear(v bool) { r.noClear = v }

// SetSortMode changes the PID table sort order.
func (r *Renderer) SetSortMode(m SortMode) { r.sortMode = m }

// CycleSortMode rotates through available sort modes.
func (r *Renderer) CycleSortMode() {
	r.sortMode = (r.sortMode + 1) % 3
}

func (r *Renderer) detectWidth() {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 40 {
		r.termWidth = w
	} else {
		r.termWidth = 92
	}
}

func sortModeName(m SortMode) string {
	switch m {
	case SortByDrops:
		return "drops"
	case SortByBitrate:
		return "bitrate"
	case SortByPID:
		return "pid"
	}
	return "?"
}

func (r *Renderer) Render(s mpegts.Snapshot) {
	r.detectWidth()
	w := r.termWidth

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

	// Per-PID bitrate & drop rate
	r.pidBitrates = make(map[uint16]float64, len(s.PIDs))
	r.pidDropRates = make(map[uint16]float64, len(s.PIDs))
	if !r.lastTime.IsZero() {
		dt := now.Sub(r.lastTime).Seconds()
		if dt > 0 {
			lastMap := make(map[uint16]mpegts.PIDStats, len(r.last.PIDs))
			for _, p := range r.last.PIDs {
				lastMap[p.PID] = p
			}
			for i := range s.PIDs {
				p := s.PIDs[i]
				if lp, ok := lastMap[p.PID]; ok {
					r.pidBitrates[p.PID] = float64(p.Packets-lp.Packets) * 188 * 8 / dt / 1_000_000
					r.pidDropRates[p.PID] = float64(p.Drops-lp.Drops) / dt
				}
			}
		}
	}

	r.bitrates = appendLimit(r.bitrates, mbps, 60)
	r.dropRates = appendLimit(r.dropRates, dropsPerSec, 60)
	r.last = s
	r.lastTime = now

	var buf strings.Builder

	if !r.noClear {
		buf.WriteString("\033[2J\033[H")
	}

	// Header
	headerWidth := w - 1
	if headerWidth < 40 {
		headerWidth = 40
	}

	title := fmt.Sprintf("%s ECOmpeg2ts%s  %s%s%s  up %s",
		colBold+colCyan, colReset, colDim, r.source, colReset,
		fmtDuration(elapsed))
	buf.WriteString(title + "\n")
	separator := strings.Repeat("─", min(headerWidth, 92))
	buf.WriteString(colDim + separator + colReset + "\n")

	// Summary line
	dropColour := colGreen
	if dropsPerSec > 1 {
		dropColour = colYellow
	}
	if dropsPerSec > 10 {
		dropColour = colRed
	}

	teiColour := colGreen
	if s.TEIErrors > 0 {
		teiColour = colYellow
	}
	discColour := colGreen
	if s.Discontinuities > 0 {
		discColour = colYellow
	}
	syncColour := colGreen
	if s.SyncLosses > 0 {
		syncColour = colRed
	}

	buf.WriteString(fmt.Sprintf("%spkts%s %s%d%s  %sbitrate%s %s%.2f Mbps%s  %spps%s %s%.0f%s  %sdrops%s %s%d%s  %sdup%s %d  %stei%s %s%d%s  %sdisc%s %s%d%s  %ssync%s %s%d%s\n",
		colDim, colReset, colBold, s.Packets, colReset,
		colDim, colReset, colGreen, mbps, colReset,
		colDim, colReset, colBold, pps, colReset,
		colDim, colReset, dropColour, s.Drops, colReset,
		colDim, colReset, s.Duplicates,
		colDim, colReset, teiColour, s.TEIErrors, colReset,
		colDim, colReset, discColour, s.Discontinuities, colReset,
		colDim, colReset, syncColour, s.SyncLosses, colReset,
	))

	// Sparklines
	graphWidth := min(headerWidth-20, 72)
	if graphWidth < 30 {
		graphWidth = 30
	}
	bpsSpark := sparkColoured(r.bitrates, graphWidth, mbps)
	drpSpark := sparkColoured(r.dropRates, graphWidth, dropsPerSec)
	buf.WriteString(fmt.Sprintf("%sbitrate%s [%s] %s%.2f%s Mbps\n", colDim, colReset, bpsSpark, colGreen, mbps, colReset))
	buf.WriteString(fmt.Sprintf("%sdrops/s%s [%s] %s%.1f%s\n", colDim, colReset, drpSpark, dropColour, dropsPerSec, colReset))

	buf.WriteString("\n")

	// PID table
	sortModeStr := sortModeName(r.sortMode)
	buf.WriteString(fmt.Sprintf("%sTop PIDs%s (sort: %s%s%s)\n", colBold+colCyan, colReset, colYellow, sortModeStr, colReset))
	buf.WriteString(fmt.Sprintf("%s%-7s %12s %9s %7s %5s %5s %5s %9s %9s  %s%s\n",
		colDim,
		"PID", "packets", "Mbps", "drops/s", "dup", "tei", "disc", "payload", "adaponly", "share",
		colReset))

	lineWidth := min(headerWidth, 100)
	buf.WriteString(colDim + strings.Repeat("─", lineWidth) + colReset + "\n")

	pids := append([]mpegts.PIDStats(nil), s.PIDs...)
	sort.Slice(pids, func(i, j int) bool {
		switch r.sortMode {
		case SortByBitrate:
			if r.pidBitrates[pids[i].PID] == r.pidBitrates[pids[j].PID] {
				return pids[i].Packets > pids[j].Packets
			}
			return r.pidBitrates[pids[i].PID] > r.pidBitrates[pids[j].PID]
		case SortByPID:
			return pids[i].PID < pids[j].PID
		default: // SortByDrops
			if pids[i].Drops == pids[j].Drops {
				return pids[i].Packets > pids[j].Packets
			}
			return pids[i].Drops > pids[j].Drops
		}
	})

	limit := 20
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
		bps := r.pidBitrates[p.PID]
		dps := r.pidDropRates[p.PID]

		// Colour the PID based on health
		pidColour := colGreen
		if p.Drops > 0 {
			pidColour = colYellow
		}
		if dps > 1 || p.Discontinuities > 0 || p.TEIErrors > 0 {
			pidColour = colRed
		}

		// Drop rate colour
		dpsColour := colGreen
		if dps > 0.5 {
			dpsColour = colYellow
		}
		if dps > 5 {
			dpsColour = colRed
		}

		shareBar := bar(p.Packets, maxPackets, 14)

		buf.WriteString(fmt.Sprintf("%s0x%04x%s %12d %s%8.2f%s %s%7.1f%s %5d %5d %5d %9d %9d  %s\n",
			pidColour, p.PID, colReset,
			p.Packets,
			colGreen, bps, colReset,
			dpsColour, dps, colReset,
			p.Duplicates, p.TEIErrors, p.Discontinuities,
			p.PayloadPackets, p.AdaptationOnly,
			shareColoured(shareBar, dps),
		))
	}

	buf.WriteString("\n")
	buf.WriteString(fmt.Sprintf("%sCtrl+C to stop. --json for machine output. --no-clear for SSH/tmux.%s\n", colDim, colReset))

	fmt.Fprint(r.out, buf.String())
}

func sparkColoured(values []float64, width int, current float64) string {
	if len(values) == 0 {
		return colDim + strings.Repeat(" ", width) + colReset
	}
	var max float64
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	if max <= 0 {
		return colDim + strings.Repeat("·", width) + colReset
	}
	var b strings.Builder
	start := 0
	if len(values) > width {
		start = len(values) - width
	}
	for _, v := range values[start:] {
		switch ratio := v / max; {
		case ratio > 0.80:
			b.WriteString(colGreen + "█" + colReset)
		case ratio > 0.55:
			b.WriteString(colGreen + "▓" + colReset)
		case ratio > 0.25:
			b.WriteString(colYellow + "▒" + colReset)
		case ratio > 0:
			b.WriteString(colDim + "·" + colReset)
		default:
			b.WriteString(colDim + " " + colReset)
		}
	}
	// Pad to width
	result := b.String()
	visibleLen := len(values[start:])
	if visibleLen < width {
		result += colDim + strings.Repeat(" ", width-visibleLen) + colReset
	}
	return result
}

func shareColoured(bar string, dropRate float64) string {
	if dropRate > 5 {
		return colRed + bar + colReset
	}
	if dropRate > 0.5 {
		return colYellow + bar + colReset
	}
	return colGreen + bar + colReset
}

func appendLimit(values []float64, value float64, limit int) []float64 {
	values = append(values, value)
	if len(values) > limit {
		return values[len(values)-limit:]
	}
	return values
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
	return strings.Repeat("█", n) + strings.Repeat(" ", width-n)
}

func fmtDuration(sec float64) string {
	d := time.Duration(sec) * time.Second
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%dm%02ds", m, s)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

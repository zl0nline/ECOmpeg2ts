package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zl0nline/ECOmpeg2ts/internal/dashboard"
	"github.com/zl0nline/ECOmpeg2ts/internal/input"
	"github.com/zl0nline/ECOmpeg2ts/internal/mpegts"
)

func main() {
	var (
		source        = flag.String("source", "", "input source URL, for example udp://@239.3.1.1:1234 or file:///tmp/a.ts")
		udpAddr       = flag.String("udp", "", "UDP listen address, for example :1234")
		multicastAddr = flag.String("multicast", "", "multicast group address, for example 239.10.10.10:1234")
		iface         = flag.String("iface", "", "network interface for multicast, optional")
		filePath      = flag.String("file", "", "MPEG-TS file to analyze")
		jsonMode      = flag.Bool("json", false, "emit JSON snapshots instead of the dashboard")
		noClear       = flag.Bool("no-clear", false, "do not clear screen each tick (for SSH/tmux logging)")
		sortMode      = flag.String("sort", "drops", "PID sort mode: drops, bitrate, pid")
		interval      = flag.Duration("interval", time.Second, "dashboard or JSON refresh interval")
	)
	flag.Parse()

	src, err := openSource(*source, *udpAddr, *multicastAddr, *iface, *filePath, flag.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer src.Reader.Close()

	analyzer := mpegts.NewAnalyzer()
	errCh := make(chan error, 1)
	go readLoop(src.Reader, analyzer, errCh)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	renderer := dashboard.New(os.Stdout, src.Name)
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
			s := analyzer.Snapshot()
			if *jsonMode {
				if err := encoder.Encode(s); err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
			} else {
				renderer.Render(s)
			}
		case err := <-errCh:
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			s := analyzer.Snapshot()
			if *jsonMode {
				_ = encoder.Encode(s)
			} else {
				renderer.Render(s)
			}
			return
		case <-sigCh:
			fmt.Fprintln(os.Stderr, "stopping")
			return
		}
	}
}

func openSource(source, udpAddr, multicastAddr, iface, filePath string, args []string) (*input.Source, error) {
	if source == "" && len(args) == 1 {
		source = args[0]
	}
	set := 0
	if source != "" {
		set++
	}
	if udpAddr != "" {
		set++
	}
	if multicastAddr != "" {
		set++
	}
	if filePath != "" {
		set++
	}
	if set != 1 {
		return nil, fmt.Errorf("choose exactly one input: --source, positional source, --udp, --multicast, or --file")
	}
	if len(args) > 1 {
		return nil, fmt.Errorf("expected at most one positional source")
	}
	if source != "" {
		return input.OpenSource(source, iface)
	}
	if filePath != "" {
		return input.OpenFile(filePath)
	}
	if multicastAddr != "" {
		return input.OpenMulticast(multicastAddr, iface)
	}
	return input.OpenUDP(udpAddr)
}

func readLoop(r io.Reader, analyzer *mpegts.Analyzer, errCh chan<- error) {
	buf := make([]byte, 64*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			analyzer.Feed(buf[:n])
		}
		if err != nil {
			if err == io.EOF {
				errCh <- nil
				return
			}
			errCh <- err
			return
		}
	}
}

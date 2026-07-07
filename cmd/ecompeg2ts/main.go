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
		udpAddr       = flag.String("udp", "", "UDP listen address, for example :1234")
		multicastAddr = flag.String("multicast", "", "multicast group address, for example 239.10.10.10:1234")
		iface         = flag.String("iface", "", "network interface for multicast, for example eth0")
		filePath      = flag.String("file", "", "MPEG-TS file to analyze")
		jsonMode      = flag.Bool("json", false, "emit JSON snapshots instead of the dashboard")
		interval      = flag.Duration("interval", time.Second, "dashboard or JSON refresh interval")
	)
	flag.Parse()

	src, err := openSource(*udpAddr, *multicastAddr, *iface, *filePath)
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

func openSource(udpAddr, multicastAddr, iface, filePath string) (*input.Source, error) {
	set := 0
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
		return nil, fmt.Errorf("choose exactly one input: --udp, --multicast, or --file")
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

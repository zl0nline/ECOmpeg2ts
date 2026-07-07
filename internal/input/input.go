package input

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
)

type Source struct {
	Name   string
	Reader io.ReadCloser
}

type SourceSpec struct {
	Scheme      string
	Address     string
	IsMulticast bool
	FilePath    string
}

func ParseSource(raw string) (SourceSpec, error) {
	if strings.TrimSpace(raw) == "" {
		return SourceSpec{}, fmt.Errorf("empty source")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return SourceSpec{}, err
	}
	switch u.Scheme {
	case "udp":
		addr := u.Host
		if addr == "" {
			addr = u.Opaque
		}
		if addr == "" {
			return SourceSpec{}, fmt.Errorf("udp source requires host:port")
		}
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return SourceSpec{}, fmt.Errorf("udp source requires host:port: %w", err)
		}
		ip := net.ParseIP(strings.Trim(host, "[]"))
		return SourceSpec{
			Scheme:      "udp",
			Address:     addr,
			IsMulticast: ip != nil && ip.IsMulticast(),
		}, nil
	case "file":
		path := u.Path
		if path == "" {
			return SourceSpec{}, fmt.Errorf("file source requires a path")
		}
		return SourceSpec{Scheme: "file", FilePath: path}, nil
	case "":
		return SourceSpec{Scheme: "file", FilePath: raw}, nil
	default:
		return SourceSpec{}, fmt.Errorf("unsupported source scheme %q", u.Scheme)
	}
}

func OpenSource(raw, ifaceName string) (*Source, error) {
	spec, err := ParseSource(raw)
	if err != nil {
		return nil, err
	}
	switch spec.Scheme {
	case "file":
		return OpenFile(spec.FilePath)
	case "udp":
		if spec.IsMulticast {
			return OpenMulticast(spec.Address, ifaceName)
		}
		return OpenUDP(spec.Address)
	default:
		return nil, fmt.Errorf("unsupported source scheme %q", spec.Scheme)
	}
}

func OpenFile(path string) (*Source, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &Source{Name: "file:" + path, Reader: f}, nil
}

func OpenUDP(addr string) (*Source, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}
	return &Source{Name: "udp:" + addr, Reader: conn}, nil
}

func OpenMulticast(groupAddr, ifaceName string) (*Source, error) {
	addr, err := net.ResolveUDPAddr("udp", groupAddr)
	if err != nil {
		return nil, err
	}
	var iface *net.Interface
	if ifaceName != "" {
		iface, err = net.InterfaceByName(ifaceName)
		if err != nil {
			return nil, err
		}
	}
	conn, err := net.ListenMulticastUDP("udp", iface, addr)
	if err != nil {
		return nil, err
	}
	if err := conn.SetReadBuffer(4 * 1024 * 1024); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("set UDP read buffer: %w", err)
	}
	return &Source{Name: "multicast:" + groupAddr, Reader: conn}, nil
}

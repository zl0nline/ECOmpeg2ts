package input

import (
	"fmt"
	"io"
	"net"
	"os"
)

type Source struct {
	Name   string
	Reader io.ReadCloser
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

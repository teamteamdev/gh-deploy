package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// parseBind splits a bind string into a network and address. A "unix:" prefix
// selects a Unix domain socket, otherwise a TCP address is assumed.
func parseBind(bind string) (proto, addr string) {
	if addr, ok := strings.CutPrefix(bind, "unix:"); ok {
		return "unix", addr
	}
	return "tcp", bind
}

// listen creates a listener for the given bind string, handling Unix socket
// cleanup and permissions when necessary.
func listen(bind string) (net.Listener, error) {
	proto, addr := parseBind(bind)

	if proto == "unix" {
		if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("cannot delete existing file: %w", err)
		}

		if err := os.MkdirAll(filepath.Dir(addr), 0755); err != nil {
			return nil, fmt.Errorf("cannot create directory for socket: %w", err)
		}
	}

	listener, err := net.Listen(proto, addr)
	if err != nil {
		return nil, err
	}

	if proto == "unix" {
		if err := os.Chmod(addr, 0777); err != nil {
			listener.Close()
			return nil, fmt.Errorf("cannot set permissions for socket: %w", err)
		}
	}

	return listener, nil
}

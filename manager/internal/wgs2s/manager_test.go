package wgs2s

import (
	"context"
	"fmt"
	"net"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// BUG-L11: the original checkPortAvailable only probed udp4. A WireGuard
// instance bound to [::]:port with IPV6_V6ONLY=1 (or UDP6-only listener)
// remained invisible, so manager would happily start a second tunnel on the
// same port — wireguard-go then bound only the v4 half and listened in
// silent partial state. We now probe both families.

func listenV6Only(t *testing.T, port int) net.PacketConn {
	t.Helper()
	lc := net.ListenConfig{
		Control: func(_, _ string, c syscall.RawConn) error {
			var setErr error
			if err := c.Control(func(fd uintptr) {
				setErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_V6ONLY, 1)
			}); err != nil {
				return err
			}
			return setErr
		},
	}
	pc, err := lc.ListenPacket(context.Background(), "udp6", fmt.Sprintf("[::]:%d", port))
	if err != nil {
		t.Fatalf("could not bind udp6 with V6ONLY: %v", err)
	}
	return pc
}

func freeUDPPort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freeUDPPort: %v", err)
	}
	defer func() { _ = c.Close() }()
	return c.LocalAddr().(*net.UDPAddr).Port
}

func TestCheckPortAvailable_DetectsV6OnlyBinding(t *testing.T) {
	port := freeUDPPort(t)

	pc := listenV6Only(t, port)
	t.Cleanup(func() { _ = pc.Close() })

	if err := checkPortAvailable(port); err == nil {
		t.Fatalf("checkPortAvailable(%d) returned nil; expected error because port is bound on [::]:%d", port, port)
	}
}

func TestCheckPortAvailable_DetectsV4Binding(t *testing.T) {
	port := freeUDPPort(t)

	pc, err := net.ListenPacket("udp4", fmt.Sprintf(":%d", port))
	if err != nil {
		t.Fatalf("bind udp4: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	if err := checkPortAvailable(port); err == nil {
		t.Fatalf("checkPortAvailable(%d) returned nil; expected error because port is bound on 0.0.0.0:%d", port, port)
	}
}

func TestCheckPortAvailable_FreePort(t *testing.T) {
	port := freeUDPPort(t)
	if err := checkPortAvailable(port); err != nil {
		t.Fatalf("checkPortAvailable(%d) reported error on a free port: %v", port, err)
	}
}

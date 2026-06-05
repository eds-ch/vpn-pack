package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
)

var errNoSocketPath = errors.New("socket path required (no TCP fallback)")

// systemdListener returns the listener inherited from systemd via socket
// activation, or (nil, nil) if the process was not socket-activated.
//
// Protocol (man systemd.socket / sd_listen_fds(3)):
//   - LISTEN_PID must equal getpid() — protects against env-var leakage
//     into child processes.
//   - LISTEN_FDS gives the count of inherited fds, starting at fd 3.
//
// We expect exactly one socket; more than one is a misconfiguration we
// refuse to silently accept. On return the env vars are unset so they
// don't leak into our own child processes.
func systemdListener() (net.Listener, error) {
	pid := os.Getenv("LISTEN_PID")
	if pid == "" || pid != strconv.Itoa(os.Getpid()) {
		return nil, nil
	}
	n, err := strconv.Atoi(os.Getenv("LISTEN_FDS"))
	if err != nil || n < 1 {
		return nil, nil
	}
	// Always clear so child processes don't see leftovers.
	defer func() {
		_ = os.Unsetenv("LISTEN_PID")
		_ = os.Unsetenv("LISTEN_FDS")
		_ = os.Unsetenv("LISTEN_FDNAMES")
	}()
	if n != 1 {
		return nil, fmt.Errorf("systemd passed %d sockets, want exactly 1", n)
	}
	f := os.NewFile(uintptr(3), "systemd-activated-socket")
	if f == nil {
		return nil, fmt.Errorf("systemd LISTEN_FDS=%d but fd 3 is not valid", n)
	}
	ln, err := net.FileListener(f)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("adopt systemd fd 3: %w", err)
	}
	// net.FileListener dup's the fd; close our copy.
	if err := f.Close(); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("close inherited fd: %w", err)
	}
	return ln, nil
}

// openManagerSocket creates the manager's unix-socket listener at path.
// Used only in the dev/standalone path where systemd socket activation
// is not in play. The production deployment path delegates socket
// creation to systemd (vpn-pack-manager.socket) and CAP_CHOWN is then
// not required of the manager process — see GAP-002 in RELEASE-CHECKLIST.md.
//
// It ensures the parent directory exists, removes any stale path, chmods
// the socket to 0660 so a group peer (nginx) can connect, and best-effort
// chowns the socket to group `nginx` if that group exists on the host.
// Returns the listener; caller owns Close().
func openManagerSocket(path string) (net.Listener, error) {
	if path == "" {
		return nil, errNoSocketPath
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create socket dir: %w", err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove stale socket: %w", err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen unix %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o660); err != nil { //nolint:gosec // G302: 0660 is intentional — nginx group needs connect(2); PeerUIDAuth still gates access
		_ = ln.Close()
		return nil, fmt.Errorf("chmod socket: %w", err)
	}
	// Best-effort: chown the socket to group `nginx` so the nginx worker
	// (group `nginx` on UDM-SE) can connect(2). Missing group is non-fatal;
	// PeerUIDAuth at the app layer still gates access.
	if g, err := user.LookupGroup("nginx"); err == nil {
		if gid, perr := strconv.Atoi(g.Gid); perr == nil {
			if cerr := os.Chown(path, -1, gid); cerr != nil {
				slog.Warn("socket chown to nginx group failed; nginx may be unable to connect", "err", cerr)
			}
		}
	} else {
		slog.Warn("nginx group not found on this host; socket remains root:root", "err", err)
	}
	return ln, nil
}

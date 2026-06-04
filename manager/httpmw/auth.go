package httpmw

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/user"
	"strconv"

	"golang.org/x/sys/unix"
)

type peerUIDKey struct{}

func withPeerUID(ctx context.Context, uid uint32) context.Context {
	return context.WithValue(ctx, peerUIDKey{}, uid)
}

func peerUID(ctx context.Context) (uint32, bool) {
	v, ok := ctx.Value(peerUIDKey{}).(uint32)
	return v, ok
}

// ConnContext is plugged into http.Server.ConnContext. It attaches the peer
// uid to the context for unix sockets. TCP connections (test http servers,
// future deployments) carry no peer-uid and PeerUIDAuth will 403 them.
func ConnContext(ctx context.Context, c net.Conn) context.Context {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return ctx
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return ctx
	}
	var ucred *unix.Ucred
	_ = raw.Control(func(fd uintptr) {
		ucred, _ = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if ucred == nil {
		return ctx
	}
	return withPeerUID(ctx, ucred.Uid)
}

// PeerUIDAuth accepts only requests from connections whose peer uid matches
// any of the allowed uids. Requests with NO peer-uid context (TCP, no
// SO_PEERCRED) are rejected outright. The current process euid is always
// allowed.
//
// On UDM-SE nginx workers run as user `nginx`, NOT root — the boot path must
// pass the nginx uid via LookupAllowedUIDs("nginx") or proxy requests are 403.
func PeerUIDAuth(allowed ...uint32) Middleware {
	allow := map[uint32]struct{}{}
	for _, u := range allowed {
		allow[u] = struct{}{}
	}
	allow[uint32(os.Geteuid())] = struct{}{}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uid, ok := peerUID(r.Context())
			if !ok {
				http.Error(w, "forbidden: no peer credentials", http.StatusForbidden)
				return
			}
			if _, ok := allow[uid]; !ok {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// LookupAllowedUIDs returns the set of UIDs allowed to connect to the
// manager unix socket. Always includes uid 0 (root) and the current process
// euid. Additionally tries to resolve the listed user names (e.g. "nginx")
// and includes each found uid. Missing users are NOT an error — the caller
// gets back the uids that exist on this host.
func LookupAllowedUIDs(userNames ...string) []uint32 {
	out := []uint32{0, uint32(os.Geteuid())}
	for _, name := range userNames {
		u, err := user.Lookup(name)
		if err != nil {
			continue
		}
		n, err := strconv.ParseUint(u.Uid, 10, 32)
		if err != nil {
			continue
		}
		out = append(out, uint32(n))
	}
	return out
}

// WithFakePeerUIDForTests returns a ConnContext that injects the given uid.
// Tests only. Do not call from production code.
func WithFakePeerUIDForTests(uid uint32) func(context.Context, net.Conn) context.Context {
	return func(ctx context.Context, _ net.Conn) context.Context {
		return withPeerUID(ctx, uid)
	}
}

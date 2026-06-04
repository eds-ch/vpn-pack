package udapi

import "sync"

// UDAPI exposes no ETag/version for ipset documents, so a concurrent
// GET-modify-PUT loses updates whenever two callers read the same
// baseline before either writes it back. ipsetRMW serialises the
// read-modify-write pair per set name across all goroutines in the
// process.
type ipsetRMW struct {
	mu  sync.Mutex
	per map[string]*sync.Mutex
}

var rmwState = &ipsetRMW{per: map[string]*sync.Mutex{}}

func (r *ipsetRMW) lock(name string) func() {
	r.mu.Lock()
	m, ok := r.per[name]
	if !ok {
		m = &sync.Mutex{}
		r.per[name] = m
	}
	r.mu.Unlock()
	m.Lock()
	return m.Unlock
}

// WithIpsetRMW serialises GET-modify-PUT for a single set name. f sees
// the set exclusively for its duration relative to other WithIpsetRMW
// callers on the same name. Cross-process callers (cleanup binary,
// other admins) are still racing — this only fixes intra-process
// concurrency, which is where every reported lost-update has occurred.
func WithIpsetRMW(name string, f func() error) error {
	unlock := rmwState.lock(name)
	defer unlock()
	return f()
}

package wgs2s

import "net"

type routeOwner struct {
	tunnelID string
	ifIndex  uint32
}

// routeRefCounter tracks shared ownership of kernel routes across S2S tunnels.
// Linux kernel identifies route uniqueness by (dst, table, tos, priority) without oif,
// so two tunnels adding the same CIDR get EEXIST for the second one. Without ref-counting,
// deleting the first tunnel removes the route for both.
//
// Not thread-safe — callers must hold TunnelManager.mu.
type routeRefCounter struct {
	owners map[string][]routeOwner
}

func newRouteRefCounter() *routeRefCounter {
	return &routeRefCounter{owners: make(map[string][]routeOwner)}
}

func normalizeCIDR(cidr string) string {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return cidr
	}
	return ipNet.String()
}

// add registers a tunnel as owner of a CIDR route.
// Returns true if this is the first owner (caller should add route to kernel).
func (rc *routeRefCounter) add(cidr, tunnelID string, ifIndex uint32) bool {
	key := normalizeCIDR(cidr)
	for i, o := range rc.owners[key] {
		if o.tunnelID == tunnelID {
			rc.owners[key][i].ifIndex = ifIndex
			return len(rc.owners[key]) == 1
		}
	}
	first := len(rc.owners[key]) == 0
	rc.owners[key] = append(rc.owners[key], routeOwner{tunnelID: tunnelID, ifIndex: ifIndex})
	return first
}

// remove unregisters a tunnel as owner of a CIDR route.
// Returns remaining owners after removal.
func (rc *routeRefCounter) remove(cidr, tunnelID string) []routeOwner {
	key := normalizeCIDR(cidr)
	owners := rc.owners[key]
	for i, o := range owners {
		if o.tunnelID == tunnelID {
			rc.owners[key] = append(owners[:i], owners[i+1:]...)
			if len(rc.owners[key]) == 0 {
				delete(rc.owners, key)
				return nil
			}
			return rc.owners[key]
		}
	}
	return owners
}

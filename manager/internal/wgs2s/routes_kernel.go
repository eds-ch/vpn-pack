package wgs2s

import "github.com/jsimonetti/rtnetlink"

// routeOps abstracts kernel route mutations so tests can substitute a fake.
// Production wires this to rtnetlinkRoutes which forwards to rtnetlink.Conn.
type routeOps interface {
	Add(*rtnetlink.RouteMessage) error
	Delete(*rtnetlink.RouteMessage) error
	Replace(*rtnetlink.RouteMessage) error
}

type rtnetlinkRoutes struct {
	conn *rtnetlink.Conn
}

func (r *rtnetlinkRoutes) Add(msg *rtnetlink.RouteMessage) error {
	return r.conn.Route.Add(msg)
}

func (r *rtnetlinkRoutes) Delete(msg *rtnetlink.RouteMessage) error {
	return r.conn.Route.Delete(msg)
}

func (r *rtnetlinkRoutes) Replace(msg *rtnetlink.RouteMessage) error {
	return r.conn.Route.Replace(msg)
}

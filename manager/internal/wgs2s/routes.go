package wgs2s

import (
	"errors"
	"fmt"
	"log/slog"
	"net"

	"github.com/jsimonetti/rtnetlink"
	"golang.org/x/sys/unix"
)

const routeMetric = 100

func buildRouteMessage(cidr string, ifIndex uint32) (*rtnetlink.RouteMessage, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("parse route CIDR %s: %w", cidr, err)
	}

	ones, _ := ipNet.Mask.Size()
	family := uint8(unix.AF_INET)
	if ipNet.IP.To4() == nil {
		family = unix.AF_INET6
	}

	dst := ipNet.IP
	if family == unix.AF_INET {
		dst = dst.To4()
	}

	return &rtnetlink.RouteMessage{
		Family:    family,
		DstLength: uint8(ones),
		Table:     unix.RT_TABLE_MAIN,
		Protocol:  unix.RTPROT_STATIC,
		Scope:     unix.RT_SCOPE_LINK,
		Type:      unix.RTN_UNICAST,
		Attributes: rtnetlink.RouteAttributes{
			Dst:      dst,
			OutIface: ifIndex,
			Priority: routeMetric,
		},
	}, nil
}

func addRoutes(conn *rtnetlink.Conn, ifIndex uint32, cidrs []string, log *slog.Logger) error {
	for _, cidr := range cidrs {
		msg, err := buildRouteMessage(cidr, ifIndex)
		if err != nil {
			return err
		}
		if err := conn.Route.Add(msg); err != nil {
			if errors.Is(err, unix.EEXIST) {
				log.Debug("route already exists, skipping", "cidr", cidr)
				continue
			}
			return fmt.Errorf("add route %s: %w", cidr, err)
		}
	}
	return nil
}

func deleteRoutes(conn *rtnetlink.Conn, ifIndex uint32, cidrs []string) error {
	for _, cidr := range cidrs {
		msg, err := buildRouteMessage(cidr, ifIndex)
		if err != nil {
			continue
		}
		if err := conn.Route.Delete(msg); err != nil && !errors.Is(err, unix.ESRCH) {
			return fmt.Errorf("delete route %s: %w", cidr, err)
		}
	}
	return nil
}

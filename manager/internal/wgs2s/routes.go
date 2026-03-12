package wgs2s

import (
	"errors"
	"fmt"
	"log/slog"
	"net"

	"github.com/jsimonetti/rtnetlink"
	"golang.org/x/sys/unix"
)

func effectiveMetric(metric int) int {
	if metric <= 0 {
		return defaultRouteMetric
	}
	return metric
}

func buildRouteMessage(cidr string, ifIndex uint32, metric int) (*rtnetlink.RouteMessage, error) {
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
			Priority: uint32(metric),
		},
	}, nil
}

func deleteRoutes(conn *rtnetlink.Conn, ifIndex uint32, cidrs []string, metric int) error {
	m := effectiveMetric(metric)
	for _, cidr := range cidrs {
		msg, err := buildRouteMessage(cidr, ifIndex, m)
		if err != nil {
			slog.Warn("delete route: invalid CIDR", "cidr", cidr, "err", err)
			continue
		}
		if err := conn.Route.Delete(msg); err != nil && !errors.Is(err, unix.ESRCH) {
			return fmt.Errorf("delete route %s: %w", cidr, err)
		}
	}
	return nil
}

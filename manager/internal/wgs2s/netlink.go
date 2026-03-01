package wgs2s

import (
	"fmt"
	"net"

	"github.com/jsimonetti/rtnetlink"
	"golang.org/x/sys/unix"
)

func createInterface(conn *rtnetlink.Conn, name string) (uint32, error) {
	err := conn.Link.New(&rtnetlink.LinkMessage{
		Attributes: &rtnetlink.LinkAttributes{
			Name: name,
			Info: &rtnetlink.LinkInfo{
				Kind: "wireguard",
			},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("create interface %s: %w", name, err)
	}

	iface, err := net.InterfaceByName(name)
	if err != nil {
		return 0, fmt.Errorf("interface %s created but not found: %w", name, err)
	}
	return uint32(iface.Index), nil
}

func deleteInterface(conn *rtnetlink.Conn, index uint32) error {
	return conn.Link.Delete(index)
}

func setInterfaceUp(conn *rtnetlink.Conn, index uint32) error {
	return conn.Link.Set(&rtnetlink.LinkMessage{
		Family: unix.AF_UNSPEC,
		Index:  index,
		Flags:  unix.IFF_UP,
		Change: unix.IFF_UP,
	})
}

func setMTU(conn *rtnetlink.Conn, index uint32, mtu uint32) error {
	return conn.Link.Set(&rtnetlink.LinkMessage{
		Family: unix.AF_UNSPEC,
		Index:  index,
		Attributes: &rtnetlink.LinkAttributes{
			MTU: mtu,
		},
	})
}

func addAddress(conn *rtnetlink.Conn, index uint32, cidr string) error {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("parse CIDR %s: %w", cidr, err)
	}

	ones, _ := ipNet.Mask.Size()
	family := uint8(unix.AF_INET)
	addr := ip.To4()
	if addr == nil {
		family = unix.AF_INET6
		addr = ip.To16()
	}

	return conn.Address.New(&rtnetlink.AddressMessage{
		Family:       family,
		PrefixLength: uint8(ones),
		Index:        index,
		Attributes: &rtnetlink.AddressAttributes{
			Address: addr,
			Local:   addr,
		},
	})
}

func getInterfaceIndex(name string) (uint32, bool) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return 0, false
	}
	return uint32(iface.Index), true
}

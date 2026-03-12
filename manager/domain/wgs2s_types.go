package domain

import "time"

type TunnelConfig struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	InterfaceName       string    `json:"interfaceName"`
	ListenPort          int       `json:"listenPort"`
	TunnelAddress       string    `json:"tunnelAddress"`
	PeerPublicKey       string    `json:"peerPublicKey"`
	PeerEndpoint        string    `json:"peerEndpoint"`
	AllowedIPs          []string  `json:"allowedIPs"`
	LocalSubnets        []string  `json:"localSubnets,omitempty"`
	PersistentKeepalive int       `json:"persistentKeepalive"`
	MTU                 int       `json:"mtu"`
	RouteMetric         int       `json:"routeMetric,omitempty"`
	Enabled             bool      `json:"enabled"`
	CreatedAt           time.Time `json:"createdAt"`
}

type WgS2sStatus struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	InterfaceName string    `json:"interfaceName"`
	Enabled       bool      `json:"enabled"`
	Connected     bool      `json:"connected"`
	LastHandshake time.Time `json:"lastHandshake"`
	TransferRx    int64     `json:"transferRx"`
	TransferTx    int64     `json:"transferTx"`
	Endpoint      string    `json:"endpoint"`
	ListenPort    int       `json:"listenPort"`
	LocalAddress  string    `json:"localAddress"`
	RemoteSubnets []string  `json:"remoteSubnets"`
	ForwardINOk   bool      `json:"forwardINOk"`
}

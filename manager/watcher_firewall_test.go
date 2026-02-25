package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSendFirewallRequest(t *testing.T) {
	t.Run("normal send", func(t *testing.T) {
		s := &Server{
			firewallCh: make(chan FirewallRequest, 2),
		}
		s.sendFirewallRequest(FirewallRequest{Action: "apply-wg-s2s", Interface: "wg0"})
		assert.Len(t, s.firewallCh, 1)
		req := <-s.firewallCh
		assert.Equal(t, "apply-wg-s2s", req.Action)
		assert.Equal(t, "wg0", req.Interface)
	})

	t.Run("channel full does not block", func(t *testing.T) {
		s := &Server{
			firewallCh: make(chan FirewallRequest, 1),
		}
		s.sendFirewallRequest(FirewallRequest{Action: "first"})
		s.sendFirewallRequest(FirewallRequest{Action: "second"})
		assert.Len(t, s.firewallCh, 1)
		req := <-s.firewallCh
		assert.Equal(t, "first", req.Action)
	})

	t.Run("unbuffered channel no reader", func(t *testing.T) {
		s := &Server{
			firewallCh: make(chan FirewallRequest),
		}
		s.sendFirewallRequest(FirewallRequest{Action: "test"})
	})
}

package service

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestChecker(opts ...func(*RoutingHealthChecker)) *RoutingHealthChecker {
	c := &RoutingHealthChecker{
		ifaceExists:   func(string) bool { return true },
		readRPFilter:  func(string) (int, error) { return 0, nil },
		listFwRules:   func() ([]PBRInfo, error) { return nil, nil },
		checkIP6Chain: func(string) bool { return true },
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func TestRoutingHealth_NilReceiver(t *testing.T) {
	var c *RoutingHealthChecker
	assert.Nil(t, c.Check())
}

func TestRoutingHealth_NoInterface(t *testing.T) {
	c := newTestChecker(func(c *RoutingHealthChecker) {
		c.ifaceExists = func(string) bool { return false }
	})
	assert.Nil(t, c.Check())
}

func TestRoutingHealth_AllClean(t *testing.T) {
	c := newTestChecker(func(c *RoutingHealthChecker) {
		c.readRPFilter = func(string) (int, error) { return 2, nil }
	})
	assert.Nil(t, c.Check())
}

func TestRoutingHealth_RPFilterStrict(t *testing.T) {
	c := newTestChecker(func(c *RoutingHealthChecker) {
		c.readRPFilter = func(string) (int, error) { return 1, nil }
	})
	rh := c.Check()
	require.NotNil(t, rh)
	require.Len(t, rh.Warnings, 1)
	w := rh.Warnings[0]
	assert.Equal(t, "rp_filter", w.Check)
	assert.Equal(t, "warning", w.Severity)
	assert.Equal(t, "1", w.Value)
	assert.Contains(t, w.Message, "rp_filter=1")
	assert.Contains(t, w.Message, "CGNAT")
}

func TestRoutingHealth_RPFilterLoose(t *testing.T) {
	c := newTestChecker(func(c *RoutingHealthChecker) {
		c.readRPFilter = func(string) (int, error) { return 2, nil }
	})
	assert.Nil(t, c.Check())
}

func TestRoutingHealth_RPFilterReadError(t *testing.T) {
	c := newTestChecker(func(c *RoutingHealthChecker) {
		c.readRPFilter = func(string) (int, error) { return 0, errors.New("no such file") }
	})
	assert.Nil(t, c.Check())
}

func TestRoutingHealth_BypassMarkCollision(t *testing.T) {
	c := newTestChecker(func(c *RoutingHealthChecker) {
		c.listFwRules = func() ([]PBRInfo, error) {
			return []PBRInfo{
				{RulePriority: 32600, FwMark: 0x080000, FwMask: 0x7f0000, Table: 201},
			}, nil
		}
	})
	rh := c.Check()
	require.NotNil(t, rh)
	require.Len(t, rh.Warnings, 1)
	w := rh.Warnings[0]
	assert.Equal(t, "bypass_mark", w.Check)
	assert.Equal(t, "critical", w.Severity)
	assert.Equal(t, "0x80000", w.Value)
	assert.Contains(t, w.Message, "collides")
	assert.Contains(t, w.Message, "32600")
}

func TestRoutingHealth_BypassMarkNoCollision(t *testing.T) {
	c := newTestChecker(func(c *RoutingHealthChecker) {
		c.listFwRules = func() ([]PBRInfo, error) {
			return []PBRInfo{
				{RulePriority: 32501, FwMark: 0x6a0000, FwMask: 0x7f0000, Table: 201},
				{RulePriority: 32502, FwMark: 0x1a0000, FwMask: 0x7f0000, Table: 202},
				{RulePriority: 32503, FwMark: 0x1c0000, FwMask: 0x7f0000, Table: 203},
			}, nil
		}
	})
	assert.Nil(t, c.Check())
}

func TestRoutingHealth_BypassMarkSkipsTailscale(t *testing.T) {
	c := newTestChecker(func(c *RoutingHealthChecker) {
		c.listFwRules = func() ([]PBRInfo, error) {
			return []PBRInfo{
				{RulePriority: 5270, FwMark: 0x080000, FwMask: 0xff0000, Table: 52},
			}, nil
		}
	})
	assert.Nil(t, c.Check())
}

func TestRoutingHealth_BypassMarkListError(t *testing.T) {
	c := newTestChecker(func(c *RoutingHealthChecker) {
		c.listFwRules = func() ([]PBRInfo, error) { return nil, errors.New("rtnetlink unavailable") }
	})
	assert.Nil(t, c.Check())
}

func TestRoutingHealth_IPv6ChainMissing(t *testing.T) {
	c := newTestChecker(func(c *RoutingHealthChecker) {
		c.checkIP6Chain = func(string) bool { return false }
	})
	rh := c.Check()
	require.NotNil(t, rh)
	require.Len(t, rh.Warnings, 1)
	w := rh.Warnings[0]
	assert.Equal(t, "ipv6_ts_forward", w.Check)
	assert.Equal(t, "warning", w.Severity)
	assert.Contains(t, w.Message, "ip6tables")
}

func TestRoutingHealth_IPv6ChainPresent(t *testing.T) {
	c := newTestChecker(func(c *RoutingHealthChecker) {
		c.checkIP6Chain = func(string) bool { return true }
	})
	assert.Nil(t, c.Check())
}

func TestRoutingHealth_MultipleWarnings(t *testing.T) {
	c := newTestChecker(func(c *RoutingHealthChecker) {
		c.readRPFilter = func(string) (int, error) { return 1, nil }
		c.checkIP6Chain = func(string) bool { return false }
	})
	rh := c.Check()
	require.NotNil(t, rh)
	assert.Len(t, rh.Warnings, 2)

	checks := map[string]bool{}
	for _, w := range rh.Warnings {
		checks[w.Check] = true
	}
	assert.True(t, checks["rp_filter"])
	assert.True(t, checks["ipv6_ts_forward"])
}

func TestRoutingHealth_CacheTTL(t *testing.T) {
	calls := 0
	c := newTestChecker(func(c *RoutingHealthChecker) {
		c.readRPFilter = func(string) (int, error) {
			calls++
			return 1, nil
		}
	})

	rh1 := c.Check()
	require.NotNil(t, rh1)
	assert.Equal(t, 1, calls)

	rh2 := c.Check()
	assert.Equal(t, rh1, rh2)
	assert.Equal(t, 1, calls, "second call should return cache")

	c.mu.Lock()
	c.cachedAt = time.Now().Add(-2 * routingHealthTTL)
	c.mu.Unlock()

	c.Check()
	assert.Equal(t, 2, calls, "call after TTL should re-check")
}

func TestRoutingHealth_CacheHealthyState(t *testing.T) {
	calls := 0
	c := newTestChecker(func(c *RoutingHealthChecker) {
		c.readRPFilter = func(string) (int, error) {
			calls++
			return 2, nil
		}
	})

	rh1 := c.Check()
	assert.Nil(t, rh1)
	assert.Equal(t, 1, calls)

	rh2 := c.Check()
	assert.Nil(t, rh2)
	assert.Equal(t, 1, calls, "healthy result should be cached too")

	c.mu.Lock()
	c.cachedAt = time.Now().Add(-2 * routingHealthTTL)
	c.mu.Unlock()

	c.Check()
	assert.Equal(t, 2, calls, "call after TTL should re-check")
}

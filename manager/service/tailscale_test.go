package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
)

// --- Mocks ---

type mockTailscaleClient struct {
	statusFn                func(ctx context.Context) (*ipnstate.Status, error)
	editPrefsFn             func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error)
	startLoginInteractiveFn func(ctx context.Context) error
	logoutFn                func(ctx context.Context) error
}

func (m *mockTailscaleClient) Status(ctx context.Context) (*ipnstate.Status, error) {
	if m.statusFn != nil {
		return m.statusFn(ctx)
	}
	return &ipnstate.Status{BackendState: "Running"}, nil
}

func (m *mockTailscaleClient) EditPrefs(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
	if m.editPrefsFn != nil {
		return m.editPrefsFn(ctx, mp)
	}
	return &ipn.Prefs{}, nil
}

func (m *mockTailscaleClient) StartLoginInteractive(ctx context.Context) error {
	if m.startLoginInteractiveFn != nil {
		return m.startLoginInteractiveFn(ctx)
	}
	return nil
}

func (m *mockTailscaleClient) Logout(ctx context.Context) error {
	if m.logoutFn != nil {
		return m.logoutFn(ctx)
	}
	return nil
}

type mockTailscaleFirewall struct {
	integrationReadyFn func() bool
}

func (m *mockTailscaleFirewall) IntegrationReady() bool {
	if m.integrationReadyFn != nil {
		return m.integrationReadyFn()
	}
	return true
}

// --- Factory ---

func newTestTailscaleService(opts ...func(*TailscaleService)) *TailscaleService {
	svc := &TailscaleService{
		ts: &mockTailscaleClient{},
		fw: &mockTailscaleFirewall{},
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// --- Activate tests ---

func TestActivate_IntegrationNotReady(t *testing.T) {
	svc := newTestTailscaleService(func(s *TailscaleService) {
		s.fw = &mockTailscaleFirewall{
			integrationReadyFn: func() bool { return false },
		}
	})

	err := svc.Activate(context.Background())
	require.Error(t, err)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrPrecondition, se.Kind)
	assert.Equal(t, ErrMsgIntegrationKeyRequired, se.Message)
}

func TestActivate_NilFirewall(t *testing.T) {
	svc := newTestTailscaleService(func(s *TailscaleService) {
		s.fw = nil
	})

	err := svc.Activate(context.Background())
	require.Error(t, err)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrPrecondition, se.Kind)
}

func TestActivate_StatusError(t *testing.T) {
	svc := newTestTailscaleService(func(s *TailscaleService) {
		s.ts = &mockTailscaleClient{
			statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
				return nil, errors.New("connection refused")
			},
		}
	})

	err := svc.Activate(context.Background())
	require.Error(t, err)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrUpstream, se.Kind)
}

func TestActivate_NeedsLogin(t *testing.T) {
	editCalled := false
	loginCalled := false
	svc := newTestTailscaleService(func(s *TailscaleService) {
		s.ts = &mockTailscaleClient{
			statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
				return &ipnstate.Status{BackendState: "NeedsLogin"}, nil
			},
			editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
				editCalled = true
				assert.True(t, mp.CorpDNSSet)
				assert.False(t, mp.Prefs.CorpDNS)
				return &ipn.Prefs{}, nil
			},
			startLoginInteractiveFn: func(ctx context.Context) error {
				loginCalled = true
				return nil
			},
		}
	})

	err := svc.Activate(context.Background())
	require.NoError(t, err)
	assert.True(t, editCalled, "should disable CorpDNS")
	assert.True(t, loginCalled, "should start interactive login")
}

func TestActivate_AlreadyAuthenticated(t *testing.T) {
	editCalled := false
	svc := newTestTailscaleService(func(s *TailscaleService) {
		s.ts = &mockTailscaleClient{
			statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
				return &ipnstate.Status{BackendState: "Stopped"}, nil
			},
			editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
				editCalled = true
				assert.True(t, mp.WantRunningSet)
				assert.True(t, mp.Prefs.WantRunning)
				return &ipn.Prefs{}, nil
			},
		}
	})

	err := svc.Activate(context.Background())
	require.NoError(t, err)
	assert.True(t, editCalled, "should set WantRunning=true")
}

func TestActivate_DisableCorpDNSFails(t *testing.T) {
	svc := newTestTailscaleService(func(s *TailscaleService) {
		s.ts = &mockTailscaleClient{
			statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
				return &ipnstate.Status{BackendState: "NeedsLogin"}, nil
			},
			editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
				return nil, errors.New("edit failed")
			},
		}
	})

	err := svc.Activate(context.Background())
	require.Error(t, err)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrUpstream, se.Kind)
}

// --- Deactivate tests ---

func TestDeactivate_Success(t *testing.T) {
	editCalled := false
	svc := newTestTailscaleService(func(s *TailscaleService) {
		s.ts = &mockTailscaleClient{
			editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
				editCalled = true
				assert.True(t, mp.WantRunningSet)
				assert.False(t, mp.Prefs.WantRunning)
				return &ipn.Prefs{}, nil
			},
		}
	})

	err := svc.Deactivate(context.Background())
	require.NoError(t, err)
	assert.True(t, editCalled)
}

func TestDeactivate_Error(t *testing.T) {
	svc := newTestTailscaleService(func(s *TailscaleService) {
		s.ts = &mockTailscaleClient{
			editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
				return nil, errors.New("api error")
			},
		}
	})

	err := svc.Deactivate(context.Background())
	require.Error(t, err)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrUpstream, se.Kind)
}

// --- Login tests ---

func TestLogin_IntegrationNotReady(t *testing.T) {
	svc := newTestTailscaleService(func(s *TailscaleService) {
		s.fw = &mockTailscaleFirewall{
			integrationReadyFn: func() bool { return false },
		}
	})

	err := svc.Login(context.Background())
	require.Error(t, err)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrPrecondition, se.Kind)
}

func TestLogin_Success(t *testing.T) {
	dnsCalled := false
	loginCalled := false
	svc := newTestTailscaleService(func(s *TailscaleService) {
		s.ts = &mockTailscaleClient{
			editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
				dnsCalled = true
				return &ipn.Prefs{}, nil
			},
			startLoginInteractiveFn: func(ctx context.Context) error {
				loginCalled = true
				return nil
			},
		}
	})

	err := svc.Login(context.Background())
	require.NoError(t, err)
	assert.True(t, dnsCalled)
	assert.True(t, loginCalled)
}

func TestLogin_StartLoginFails(t *testing.T) {
	svc := newTestTailscaleService(func(s *TailscaleService) {
		s.ts = &mockTailscaleClient{
			startLoginInteractiveFn: func(ctx context.Context) error {
				return errors.New("login failed")
			},
		}
	})

	err := svc.Login(context.Background())
	require.Error(t, err)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrUpstream, se.Kind)
}

// --- Logout tests ---

func TestLogout_Success(t *testing.T) {
	logoutCalled := false
	svc := newTestTailscaleService(func(s *TailscaleService) {
		s.ts = &mockTailscaleClient{
			logoutFn: func(ctx context.Context) error {
				logoutCalled = true
				return nil
			},
		}
	})

	err := svc.Logout(context.Background())
	require.NoError(t, err)
	assert.True(t, logoutCalled)
}

func TestLogout_Error(t *testing.T) {
	svc := newTestTailscaleService(func(s *TailscaleService) {
		s.ts = &mockTailscaleClient{
			logoutFn: func(ctx context.Context) error {
				return errors.New("logout failed")
			},
		}
	})

	err := svc.Logout(context.Background())
	require.Error(t, err)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrUpstream, se.Kind)
}

package service

import (
	"context"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
)

type TailscaleClient interface {
	Status(ctx context.Context) (*ipnstate.Status, error)
	EditPrefs(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error)
	StartLoginInteractive(ctx context.Context) error
	Logout(ctx context.Context) error
}

type TailscaleFirewall interface {
	IntegrationReady() bool
}

type TailscaleService struct {
	ts TailscaleClient
	fw TailscaleFirewall
}

func NewTailscaleService(ts TailscaleClient, fw TailscaleFirewall) *TailscaleService {
	return &TailscaleService{ts: ts, fw: fw}
}

func (svc *TailscaleService) Activate(ctx context.Context) error {
	if !svc.integrationReady() {
		return &Error{Kind: ErrPrecondition, Message: ErrMsgIntegrationKeyRequired}
	}

	st, err := svc.ts.Status(ctx)
	if err != nil {
		return upstreamError(humanizeLocalAPIError(err), err)
	}

	if st.BackendState == "NeedsLogin" {
		if err := svc.disableCorpDNS(ctx); err != nil {
			return upstreamError(humanizeLocalAPIError(err), err)
		}
		if err := svc.ts.StartLoginInteractive(ctx); err != nil {
			return upstreamError(humanizeLocalAPIError(err), err)
		}
		return nil
	}

	_, err = svc.ts.EditPrefs(ctx, &ipn.MaskedPrefs{
		Prefs:          ipn.Prefs{WantRunning: true},
		WantRunningSet: true,
	})
	if err != nil {
		return upstreamError(humanizeLocalAPIError(err), err)
	}
	return nil
}

func (svc *TailscaleService) Deactivate(ctx context.Context) error {
	_, err := svc.ts.EditPrefs(ctx, &ipn.MaskedPrefs{
		Prefs:          ipn.Prefs{WantRunning: false},
		WantRunningSet: true,
	})
	if err != nil {
		return upstreamError(humanizeLocalAPIError(err), err)
	}
	return nil
}

func (svc *TailscaleService) Login(ctx context.Context) error {
	if !svc.integrationReady() {
		return &Error{Kind: ErrPrecondition, Message: ErrMsgIntegrationKeyRequired}
	}

	if err := svc.disableCorpDNS(ctx); err != nil {
		return upstreamError(humanizeLocalAPIError(err), err)
	}
	if err := svc.ts.StartLoginInteractive(ctx); err != nil {
		return upstreamError(humanizeLocalAPIError(err), err)
	}
	return nil
}

func (svc *TailscaleService) Logout(ctx context.Context) error {
	if err := svc.ts.Logout(ctx); err != nil {
		return upstreamError(humanizeLocalAPIError(err), err)
	}
	return nil
}

func (svc *TailscaleService) integrationReady() bool {
	return svc.fw != nil && svc.fw.IntegrationReady()
}

func (svc *TailscaleService) disableCorpDNS(ctx context.Context) error {
	_, err := svc.ts.EditPrefs(ctx, &ipn.MaskedPrefs{
		Prefs:      ipn.Prefs{CorpDNS: false},
		CorpDNSSet: true,
	})
	return err
}

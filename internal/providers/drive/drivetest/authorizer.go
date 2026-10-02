package drivetest

import (
	"context"
	"net/http"

	"github.com/alexandroit/LedgeSync/internal/driveauth"
)

// Authorizer forwards requests that pass the production Drive request boundary
// to the test server. It never adds credentials and rejects other accounts.
type Authorizer struct {
	server  *Server
	account string
	client  *http.Client
}

func (s *Server) Authorizer(account string) *Authorizer {
	client := s.http.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Authorizer{server: s, account: account, client: client}
}

func (a *Authorizer) DoAuthorized(ctx context.Context, account string, req *http.Request) (*http.Response, error) {
	if account != a.account {
		return nil, driveauth.ErrIdentity
	}
	if !driveauth.AllowedDriveRequest(req) {
		return nil, driveauth.ErrProvider
	}
	if ctx.Err() != nil {
		return nil, driveauth.ErrCanceled
	}
	forward := req.Clone(ctx)
	target := *req.URL
	base := a.server.URL()
	target.Scheme, target.Host = base.Scheme, base.Host
	forward.URL = &target
	forward.Host = ""
	forward.Header.Del("Authorization")
	response, err := a.client.Do(forward)
	if err != nil {
		if ctx.Err() != nil {
			return nil, driveauth.ErrCanceled
		}
		return nil, driveauth.ErrNetwork
	}
	response.Request = nil
	return response, nil
}

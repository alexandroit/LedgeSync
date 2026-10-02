package driveauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (s *Service) request(ctx context.Context, method, endpoint string, form url.Values, token string) ([]byte, int, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, 0, ErrNetwork
	}
	req.Header.Set("Accept", "application/json")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, contextError(ctx)
		}
		return nil, 0, ErrNetwork
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, resp.StatusCode, contextError(ctx)
		}
		return nil, resp.StatusCode, ErrNetwork
	}
	if len(data) > 64*1024 {
		return nil, resp.StatusCode, ErrToken
	}
	return data, resp.StatusCode, nil
}
func (s *Service) exchange(ctx context.Context, client *clientConfig, code, verifier, redirect, refresh string) (*credential, error) {
	form := url.Values{"client_id": {client.ID}, "client_secret": {client.Secret}}
	if refresh != "" {
		form.Set("grant_type", "refresh_token")
		form.Set("refresh_token", refresh)
	} else {
		form.Set("grant_type", "authorization_code")
		form.Set("code", code)
		form.Set("code_verifier", verifier)
		form.Set("redirect_uri", redirect)
	}
	data, status, err := s.request(ctx, http.MethodPost, s.tokenURL, form, "")
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &failure)
		if status == http.StatusBadRequest && (failure.Error == "invalid_grant" || failure.Error == "invalid_client" || failure.Error == "unauthorized_client") {
			return nil, ErrReconnect
		}
		if status == http.StatusTooManyRequests || status >= 500 {
			return nil, ErrNetwork
		}
		return nil, ErrToken
	}
	var response struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Type         string `json:"token_type"`
		Scope        string `json:"scope"`
		Expires      int64  `json:"expires_in"`
	}
	if json.Unmarshal(data, &response) != nil || !safeSecret(response.AccessToken) || !strings.EqualFold(response.Type, "Bearer") || response.Expires <= 0 || response.Expires > 86400 {
		return nil, ErrToken
	}
	// Initial consent must explicitly report its granted scope. A refresh may
	// omit scope per OAuth 2.0; it then retains the already validated grant.
	if !acceptedScope(response.Scope, refresh != "") {
		return nil, ErrScope
	}
	if response.RefreshToken == "" {
		response.RefreshToken = refresh
	}
	if !safeSecret(response.RefreshToken) {
		return nil, ErrToken
	}
	return &credential{AccessToken: response.AccessToken, RefreshToken: response.RefreshToken, Expiry: s.now().Add(time.Duration(response.Expires) * time.Second)}, nil
}
func (s *Service) identity(ctx context.Context, token string) (Account, error) {
	data, status, err := s.request(ctx, http.MethodGet, s.aboutURL, nil, token)
	if err != nil {
		return Account{}, err
	}
	if status == http.StatusUnauthorized {
		return Account{}, ErrReconnect
	}
	if status == http.StatusTooManyRequests || status >= 500 {
		return Account{}, ErrNetwork
	}
	if status != http.StatusOK {
		return Account{}, ErrProvider
	}
	var response struct {
		User *struct {
			Name         string `json:"displayName"`
			Email        string `json:"emailAddress"`
			PermissionID string `json:"permissionId"`
		} `json:"user"`
	}
	if json.Unmarshal(data, &response) != nil || response.User == nil {
		return Account{}, ErrProvider
	}
	u := response.User
	if !safeSecret(u.PermissionID) || len(u.PermissionID) > 256 || !safeDisplay(u.Name) || !safeDisplay(u.Email) {
		return Account{}, ErrProvider
	}
	hash := sha256.Sum256([]byte("LedgeSync:google-drive-account:v1:" + u.PermissionID))
	account := Account{Reference: "drive_" + hex.EncodeToString(hash[:]), DisplayName: u.Name, Email: u.Email}
	return account, nil
}
func validAccount(a Account) bool {
	if !strings.HasPrefix(a.Reference, "drive_") || len(a.Reference) != 70 || !safeDisplay(a.DisplayName) || !safeDisplay(a.Email) {
		return false
	}
	_, err := hex.DecodeString(a.Reference[6:])
	return err == nil
}

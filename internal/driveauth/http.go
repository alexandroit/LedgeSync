package driveauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

func (s *Service) request(ctx context.Context, method, endpoint string, form url.Values, token string) ([]byte, int, error) {
	if ctx.Err() != nil {
		return nil, 0, contextError(ctx)
	}
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
	return s.exchangeWithFallback(ctx, client, code, verifier, redirect, refresh, "")
}

// A Picker authorization-code exchange may omit a new refresh token. Its
// caller may retain an existing token only after validating the same client and
// account. This fallback never changes the grant type or allows omitted scope.
func (s *Service) exchangeWithFallback(ctx context.Context, client *clientConfig, code, verifier, redirect, refresh, retainedRefresh string) (*credential, error) {
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
		if decodeProviderJSON(data, &failure) != nil && status == http.StatusBadRequest {
			return nil, ErrToken
		}
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
	if decodeProviderJSON(data, &response) != nil || !safeSecret(response.AccessToken) || !strings.EqualFold(response.Type, "Bearer") || response.Expires <= 0 || response.Expires > 86400 {
		return nil, ErrToken
	}
	// Initial consent must explicitly report its granted scope. A refresh may
	// omit scope per OAuth 2.0; it then retains the already validated grant.
	if !acceptedScope(response.Scope, refresh != "") {
		return nil, ErrScope
	}
	if response.RefreshToken == "" {
		response.RefreshToken = refresh
		if response.RefreshToken == "" {
			response.RefreshToken = retainedRefresh
		}
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
	if status == http.StatusForbidden && insufficientScope(data) {
		return Account{}, ErrScope
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
	if decodeProviderJSON(data, &response) != nil || response.User == nil {
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

// Only an explicit authentication-scope failure invalidates the grant. A file
// ACL denial, disabled API or quota failure must not start a consent loop.
// Provider text is never returned; the existing bounded parser rejects aliases.
func insufficientScope(data []byte) bool {
	var response struct {
		Error struct {
			Errors []struct {
				Domain string `json:"domain"`
				Reason string `json:"reason"`
			} `json:"errors"`
			Details []struct {
				Type   string `json:"@type"`
				Domain string `json:"domain"`
				Reason string `json:"reason"`
			} `json:"details"`
		} `json:"error"`
	}
	if decodeProviderJSON(data, &response) != nil {
		return false
	}
	for _, item := range response.Error.Errors {
		if item.Domain == "global" && item.Reason == "insufficientPermissions" {
			return true
		}
	}
	for _, item := range response.Error.Details {
		if item.Type == "type.googleapis.com/google.rpc.ErrorInfo" && item.Domain == "googleapis.com" && item.Reason == "ACCESS_TOKEN_SCOPE_INSUFFICIENT" {
			return true
		}
	}
	return false
}
func validAccount(a Account) bool {
	if !strings.HasPrefix(a.Reference, "drive_") || len(a.Reference) != 70 || !safeDisplay(a.DisplayName) || !safeDisplay(a.Email) {
		return false
	}
	_, err := hex.DecodeString(a.Reference[6:])
	return err == nil
}

// Provider extensions are allowed, but duplicate keys must not make token,
// scope or identity interpretation depend on a JSON parser's last-key policy.
func decodeProviderJSON(data []byte, target any) error {
	if err := rejectProviderAliases(json.NewDecoder(bytes.NewReader(data)), 0); err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

// encoding/json matches struct field aliases case-insensitively. Reject both
// exact duplicates and case aliases before that decoder can choose a last value.
func rejectProviderAliases(decoder *json.Decoder, depth int) error {
	if depth > 16 {
		return ErrToken
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return ErrToken
			}
			folded := providerKey(name)
			if seen[folded] {
				return ErrToken
			}
			seen[folded] = true
			if err := rejectProviderAliases(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := rejectProviderAliases(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return ErrToken
	}
	_, err = decoder.Token()
	return err
}

// SimpleFold matches encoding/json's Unicode case-insensitive struct matching,
// including long-s and Kelvin-sign aliases that strings.ToLower does not merge.
func providerKey(name string) string {
	var folded strings.Builder
	for _, character := range name {
		representative := character
		for next := unicode.SimpleFold(character); next != character; next = unicode.SimpleFold(next) {
			if next < representative {
				representative = next
			}
		}
		folded.WriteRune(representative)
	}
	return folded.String()
}

func (s *Service) revokeToken(ctx context.Context, refresh string) error {
	if ctx.Err() != nil {
		return contextError(ctx)
	}
	// The token belongs in the form body only. No token-bearing query, redirect,
	// URL returned to the frontend, response-body logging or implicit retry.
	form := url.Values{"token": {refresh}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.revokeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return ErrRevokeFailed
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := s.http.Do(request)
	if err != nil {
		return ErrRevokeFailed
	}
	defer response.Body.Close()
	// The status is the protocol acknowledgement. Do not wait for an irrelevant
	// body after a confirmed success, or return provider-controlled error text.
	if response.StatusCode != http.StatusOK {
		return ErrRevokeFailed
	}
	return nil
}

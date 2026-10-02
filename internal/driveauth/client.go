package driveauth

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"unicode/utf8"
)

var clientIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,240}\.apps\.googleusercontent\.com$`)

// Only Google's downloaded installed-application format is accepted. Web and
// service-account credentials, arbitrary endpoints and unknown fields fail closed.
func parseClient(data []byte) (*clientConfig, error) {
	if len(data) == 0 || len(data) > 32*1024 {
		return nil, ErrClient
	}
	var doc struct {
		Installed *struct {
			ID           string   `json:"client_id"`
			ProjectID    string   `json:"project_id"`
			AuthURI      string   `json:"auth_uri"`
			TokenURI     string   `json:"token_uri"`
			CertURI      string   `json:"auth_provider_x509_cert_url"`
			Secret       string   `json:"client_secret"`
			RedirectURIs []string `json:"redirect_uris"`
		} `json:"installed"`
	}
	if strictJSON(data, &doc) != nil || doc.Installed == nil {
		return nil, ErrClient
	}
	c := doc.Installed
	if !clientIDPattern.MatchString(c.ID) || !safeSecret(c.Secret) || len(c.Secret) > 1024 {
		return nil, ErrClient
	}
	if c.AuthURI != "https://accounts.google.com/o/oauth2/auth" && c.AuthURI != authEndpoint {
		return nil, ErrClient
	}
	if c.TokenURI != tokenEndpoint || (c.CertURI != "" && c.CertURI != "https://www.googleapis.com/oauth2/v1/certs") {
		return nil, ErrClient
	}
	if len(c.RedirectURIs) == 0 || len(c.RedirectURIs) > 8 {
		return nil, ErrClient
	}
	for _, r := range c.RedirectURIs {
		u, err := url.Parse(r)
		if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" || (u.Path != "" && u.Path != "/") || (u.Host != "localhost" && u.Host != "127.0.0.1" && u.Host != "[::1]") {
			return nil, ErrClient
		}
	}
	return &clientConfig{ID: c.ID, Secret: c.Secret}, nil
}

// rejectDuplicateKeys prevents ambiguous imported or persisted documents whose
// meaning could change between parsers (encoding/json otherwise keeps the last).
func rejectDuplicateKeys(d *json.Decoder, depth int) error {
	if depth > 16 {
		return ErrClient
	}
	token, err := d.Token()
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
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return ErrClient
			}
			seen[name] = true
			if err = rejectDuplicateKeys(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err = rejectDuplicateKeys(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return ErrClient
	}
	_, err = d.Token()
	return err
}
func strictJSON(data []byte, target any) error {
	check := json.NewDecoder(bytes.NewReader(data))
	if err := rejectDuplicateKeys(check, 0); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return ErrClient
	}
	return nil
}
func safeSecret(s string) bool {
	if len(s) == 0 || len(s) > 16*1024 {
		return false
	}
	for _, c := range s {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}
func safeDisplay(s string) bool {
	if len(s) > 1024 || !utf8.ValidString(s) {
		return false
	}
	for _, c := range s {
		if c < 32 || c == 127 {
			return false
		}
	}
	return true
}

// Package domain contains transport-neutral offline contracts.
package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// cause supports errors.Is/As in Go; it is never part of Error() or JSON.
	cause error
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }
func (e *Error) Unwrap() error { return e.cause }
func Fail(code, format string, args ...any) error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Wrap returns a typed error with a safe message that still matches cause
// through errors.Is. Only Code and Message are ever displayed or serialized.
func Wrap(code, message string, cause error) error {
	return &Error{Code: code, Message: message, cause: cause}
}
func ErrorCode(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return "INTERNAL_ERROR"
}

// ValidatePath accepts portable logical paths without rewriting their spelling.
func ValidatePath(p string) error {
	if p == "" || !utf8.ValidString(p) || strings.ContainsAny(p, "\x00\\:") || strings.HasPrefix(p, "/") {
		return Fail("PATH_UNSAFE", "invalid relative path %q", p)
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return Fail("PATH_UNSAFE", "invalid path component in %q", p)
		}
	}
	return nil
}

type Provenance struct {
	Adapter        string `json:"adapter"`
	Mechanism      string `json:"mechanism"`
	Dialect        string `json:"dialect"`
	ProfileVersion string `json:"profileVersion"`
	Source         string `json:"source"`
	Line           int    `json:"line"`
	Pattern        string `json:"pattern"`
	Scope          string `json:"scope"`
	Action         string `json:"action"`
}
type GroupDecision struct {
	GroupID         string      `json:"groupId"`
	Priority        int         `json:"priority"`
	Decision        string      `json:"decision"`
	Provenance      *Provenance `json:"provenance,omitempty"`
	AncestorBlocker string      `json:"ancestorBlocker,omitempty"`
}
type Explanation struct {
	Path        string          `json:"path"`
	Kind        string          `json:"kind"`
	Decision    string          `json:"decision"`
	Reason      string          `json:"reason"`
	Composition string          `json:"composition"`
	Groups      []GroupDecision `json:"groups"`
}
type Entry struct {
	Path        string      `json:"path"`
	Name        string      `json:"name"`
	Kind        string      `json:"kind"`
	Size        int64       `json:"size"`
	ModifiedAt  string      `json:"modifiedAt"`
	Decision    string      `json:"decision"`
	Status      string      `json:"status"`
	Explanation Explanation `json:"explanation"`
	SHA256      string      `json:"sha256,omitempty"`
}
type RemoteEntry struct {
	Path           string `json:"path"`
	Kind           string `json:"kind"`
	ObjectID       string `json:"objectId"`
	Version        string `json:"version"`
	Managed        bool   `json:"managed"`
	VerifiedSHA256 string `json:"verifiedSha256"`
}
type Inventory struct {
	Identity string        `json:"identity"`
	Complete bool          `json:"complete"`
	Entries  []RemoteEntry `json:"entries"`
}
type Operation struct {
	OperationID           string   `json:"operationId"`
	Type                  string   `json:"type"`
	RelativePath          string   `json:"relativePath"`
	ObjectID              *string  `json:"objectId"`
	DependsOn             []string `json:"dependsOn"`
	ExpectedSize          int64    `json:"expectedSize"`
	SourceDigest          *string  `json:"sourceDigest"`
	ObservedRemoteVersion *string  `json:"observedRemoteVersion"`
	Explanation           string   `json:"explanation"`
}
type ScanComplete struct {
	Source      bool `json:"source"`
	Destination bool `json:"destination"`
}
type Summary struct {
	OperationCount int   `json:"operationCount"`
	UploadBytes    int64 `json:"uploadBytes"`
	TrashCount     int   `json:"trashCount"`
}
type Plan struct {
	SchemaVersion       string       `json:"schemaVersion"`
	PlanID              string       `json:"planId"`
	ProjectID           string       `json:"projectId"`
	CreatedAt           string       `json:"createdAt"`
	ExpiresAt           string       `json:"expiresAt"`
	Mode                string       `json:"mode"`
	SourceIdentity      string       `json:"sourceIdentity"`
	DestinationIdentity string       `json:"destinationIdentity"`
	ConfigDigest        string       `json:"configDigest"`
	RulesDigest         string       `json:"rulesDigest"`
	InventoryDigest     string       `json:"inventoryDigest"`
	PlanDigest          string       `json:"planDigest"`
	ScanComplete        ScanComplete `json:"scanComplete"`
	Operations          []Operation  `json:"operations"`
	Risks               []string     `json:"risks"`
	Summary             Summary      `json:"summary"`
}

// CanonicalJSON implements ledgesync-canonical-json-v1: sorted keys, UTF-8,
// no insignificant whitespace and no Unicode normalization. Plan numbers are integers.
func CanonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err = dec.Decode(&value); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	var write func(any) error
	quote := func(s string) {
		b.WriteByte('"')
		for _, r := range s {
			switch r {
			case '"', '\\':
				b.WriteByte('\\')
				b.WriteRune(r)
			case '\b':
				b.WriteString(`\b`)
			case '\f':
				b.WriteString(`\f`)
			case '\n':
				b.WriteString(`\n`)
			case '\r':
				b.WriteString(`\r`)
			case '\t':
				b.WriteString(`\t`)
			default:
				if r < 32 {
					fmt.Fprintf(&b, "\\u%04x", r)
				} else {
					b.WriteRune(r)
				}
			}
		}
		b.WriteByte('"')
	}
	write = func(x any) error {
		switch t := x.(type) {
		case nil:
			b.WriteString("null")
		case bool:
			b.WriteString(strconv.FormatBool(t))
		case string:
			quote(t)
		case json.Number:
			b.WriteString(t.String())
		case []any:
			b.WriteByte('[')
			for i, e := range t {
				if i > 0 {
					b.WriteByte(',')
				}
				if err := write(e); err != nil {
					return err
				}
			}
			b.WriteByte(']')
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			b.WriteByte('{')
			for i, k := range keys {
				if i > 0 {
					b.WriteByte(',')
				}
				quote(k)
				b.WriteByte(':')
				if err := write(t[k]); err != nil {
					return err
				}
			}
			b.WriteByte('}')
		default:
			return fmt.Errorf("unsupported canonical type %T", x)
		}
		return nil
	}
	if err := write(value); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
func HashBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func Digest(v any) (string, error) {
	b, err := CanonicalJSON(v)
	if err != nil {
		return "", err
	}
	return HashBytes(b), nil
}
func (p Plan) ComputeDigest() (string, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	var m map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err = d.Decode(&m); err != nil {
		return "", err
	}
	delete(m, "planDigest")
	return Digest(m)
}
func (p Plan) ValidateDigest() error {
	d, err := p.ComputeDigest()
	if err != nil {
		return err
	}
	if d != p.PlanDigest {
		return Fail("PLAN_STALE", "plan digest does not match its contents")
	}
	return nil
}

// ValidateEnvelope validates the transport contract, never execution authority.
func (p Plan) ValidateEnvelope() error {
	bad := func(message string) error { return Fail("CONFIG_INVALID", "invalid plan envelope: %s", message) }
	if p.SchemaVersion != "1.0" || p.PlanID == "" || p.ProjectID == "" || p.SourceIdentity == "" || p.DestinationIdentity == "" || (p.Mode != "copy" && p.Mode != "mirror") {
		return bad("identity or schema")
	}
	created, err := time.Parse(time.RFC3339Nano, p.CreatedAt)
	if err != nil {
		return bad("createdAt")
	}
	expires, err := time.Parse(time.RFC3339Nano, p.ExpiresAt)
	if err != nil || !expires.After(created) {
		return bad("expiresAt")
	}
	validHash := func(s string) bool {
		if len(s) != 64 {
			return false
		}
		_, err := hex.DecodeString(s)
		return err == nil && strings.ToLower(s) == s
	}
	for _, hash := range []string{p.ConfigDigest, p.RulesDigest, p.InventoryDigest, p.PlanDigest} {
		if !validHash(hash) {
			return bad("digest encoding")
		}
	}
	ids := map[string]bool{}
	var bytes int64
	trash := 0
	for _, o := range p.Operations {
		if o.OperationID == "" || ids[o.OperationID] || o.ExpectedSize < 0 || o.Explanation == "" {
			return bad("operation identity, size, or explanation")
		}
		if err := ValidatePath(o.RelativePath); err != nil {
			return err
		}
		if o.SourceDigest != nil && !validHash(*o.SourceDigest) {
			return bad("source digest")
		}
		dependencies := map[string]bool{}
		for _, d := range o.DependsOn {
			if !ids[d] || dependencies[d] {
				return bad("operation dependencies")
			}
			dependencies[d] = true
		}
		ids[o.OperationID] = true
		switch o.Type {
		case "upload-new":
			if o.SourceDigest == nil {
				return bad("upload without fingerprint")
			}
			bytes += o.ExpectedSize
		case "trash-managed":
			trash++
		case "create-directory", "skip", "conflict", "backup-managed", "update-managed":
		default:
			return bad("unknown operation kind")
		}
	}
	if p.Summary.OperationCount != len(p.Operations) || p.Summary.UploadBytes != bytes || p.Summary.TrashCount != trash {
		return bad("summary does not match operations")
	}
	return nil
}

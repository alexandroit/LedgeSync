// Package drivetest emulates the subset of the Google Drive v3 REST API used by
// LedgeSync, with the visibility rules of the restricted drive.file scope.
//
// It exists for deterministic local tests of the real HTTP provider, transfer
// executor and desktop bridge. It is not a general Drive implementation and it
// never contacts Google. Behaviors modeled on the real service:
//
//   - files.get("root") is not readable with drive.file unless RootReadable is set.
//     The "root" alias is still accepted as a parent and in list queries.
//   - Responses report canonical parent IDs, never the "root" alias.
//   - Only objects created by the application or explicitly granted (for example
//     through the native Picker) are visible; everything else returns 404.
//   - Resumable uploads return a session URI on www.googleapis.com, accept chunked
//     PUTs with Content-Range and reply 308 with a committed Range header.
//   - Identifiers must be reserved through files.generateIds before creation.
package drivetest

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const FolderMIME = "application/vnd.google-apps.folder"

// Object is the server-side record. Tests may inspect or mutate copies through
// the Server helpers; mutate stored objects only through those helpers.
type Object struct {
	ID             string
	Name           string
	MimeType       string
	Parents        []string
	Content        []byte
	AppProperties  map[string]string
	Trashed        bool
	CreatedByApp   bool
	Granted        bool
	DriveID        string
	CanAddChildren bool
	Version        int64
}

// Fault is applied to the next request matched by a FaultRule.
type Fault struct {
	// Status returns this HTTP status with a Drive-style error body without
	// processing the request.
	Status int
	// Reason sets the error reason for Status responses (for example rateLimitExceeded).
	Reason string
	// RetryAfter sets a Retry-After header for Status responses.
	RetryAfter string
	// DropAfterCommit processes the request and then closes the connection
	// without a response, emulating a lost acknowledgement.
	DropAfterCommit bool
	// DropBeforeCommit closes the connection without processing the request.
	DropBeforeCommit bool
	// Hook runs before the request is processed (for example to block).
	Hook func(*http.Request)
}

type faultRule struct {
	match func(*http.Request) bool
	fault Fault
	count int
}

type session struct {
	meta     createRequest
	size     int64
	crd      string
	received []byte
	done     *Object
}

type createRequest struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	MimeType      string            `json:"mimeType"`
	Parents       []string          `json:"parents"`
	AppProperties map[string]string `json:"appProperties"`
}

// Server is an httptest server plus Drive state.
type Server struct {
	mu           sync.Mutex
	http         *httptest.Server
	RootID       string
	RootReadable bool
	objects      map[string]*Object
	reserved     map[string]bool
	sessions     map[string]*session
	faults       []*faultRule
	requests     []string
	version      int64
}

// New starts a server. Close it with Close (tests usually use t.Cleanup).
func New() *Server {
	s := &Server{RootID: "0AFakeMyDriveRootFolderID", objects: map[string]*Object{}, reserved: map[string]bool{}, sessions: map[string]*session{}}
	s.objects[s.RootID] = &Object{ID: s.RootID, Name: "My Drive", MimeType: FolderMIME, CanAddChildren: true}
	s.http = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

func (s *Server) Close() { s.http.Close() }

// URL returns the base URL of the plain-HTTP test listener.
func (s *Server) URL() *url.URL { u, _ := url.Parse(s.http.URL); return u }

// Requests returns "METHOD path" entries in arrival order.
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.requests...)
}

func (s *Server) nextID(prefix string) string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}

// AddUserFolder creates a folder that the application did not create. Granted
// emulates explicit selection through the Picker under drive.file.
func (s *Server) AddUserFolder(name, parent string, granted bool) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if parent == "" || parent == "root" {
		parent = s.RootID
	}
	id := s.nextID("userFolder")
	s.version++
	s.objects[id] = &Object{ID: id, Name: name, MimeType: FolderMIME, Parents: []string{parent}, Granted: granted, CanAddChildren: true, Version: s.version}
	return id
}

// Get returns a copy of any object, visible or not.
func (s *Server) Get(id string) (Object, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.objects[id]
	if !ok {
		return Object{}, false
	}
	return clone(o), true
}

// Children returns copies of all non-trashed children of a folder, sorted by name.
func (s *Server) Children(parent string) []Object {
	s.mu.Lock()
	defer s.mu.Unlock()
	if parent == "root" {
		parent = s.RootID
	}
	var out []Object
	for _, o := range s.objects {
		if !o.Trashed && len(o.Parents) == 1 && o.Parents[0] == parent {
			out = append(out, clone(o))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name || out[i].Name == out[j].Name && out[i].ID < out[j].ID
	})
	return out
}

// Update changes stored state, for example to emulate a user trashing,
// renaming or moving a file in the Drive web interface.
func (s *Server) Update(id string, change func(*Object)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o, ok := s.objects[id]; ok {
		change(o)
		s.version++
		o.Version = s.version
	}
}

// Delete permanently removes an object (not its descendants).
func (s *Server) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, id)
}

// Inject applies fault to the next count requests accepted by match.
func (s *Server) Inject(match func(*http.Request) bool, count int, fault Fault) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faults = append(s.faults, &faultRule{match: match, fault: fault, count: count})
}

// Match helpers for Inject.
func MethodPath(method, prefix string) func(*http.Request) bool {
	return func(r *http.Request) bool { return r.Method == method && strings.HasPrefix(r.URL.Path, prefix) }
}

func clone(o *Object) Object {
	c := *o
	c.Parents = append([]string{}, o.Parents...)
	c.Content = append([]byte{}, o.Content...)
	if o.AppProperties != nil {
		c.AppProperties = map[string]string{}
		for k, v := range o.AppProperties {
			c.AppProperties[k] = v
		}
	}
	return c
}

func (s *Server) visible(o *Object) bool {
	return o != nil && (o.CreatedByApp || o.Granted || (o.ID == s.RootID && s.RootReadable))
}

func driveError(w http.ResponseWriter, status int, reason, message string) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": status, "message": message, "errors": []map[string]string{{"domain": "global", "reason": reason, "message": message}}}})
}

func (s *Server) resource(o *Object) map[string]any {
	m := map[string]any{"kind": "drive#file", "id": o.ID, "name": o.Name, "mimeType": o.MimeType, "trashed": o.Trashed, "version": strconv.FormatInt(o.Version, 10)}
	parents := []string{}
	for _, p := range o.Parents {
		parents = append(parents, p)
	}
	if len(parents) > 0 {
		m["parents"] = parents
	}
	if o.MimeType != FolderMIME {
		sum := md5.Sum(o.Content)
		m["size"] = strconv.Itoa(len(o.Content))
		m["md5Checksum"] = hex.EncodeToString(sum[:])
	}
	if len(o.AppProperties) > 0 {
		m["appProperties"] = o.AppProperties
	}
	if o.DriveID != "" {
		m["driveId"] = o.DriveID
	}
	m["capabilities"] = map[string]bool{"canAddChildren": o.MimeType == FolderMIME && o.CanAddChildren && !o.Trashed}
	return m
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) takeFault(r *http.Request) (Fault, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, rule := range s.faults {
		if rule.count > 0 && rule.match(r) {
			rule.count--
			if rule.count == 0 {
				s.faults = append(s.faults[:i], s.faults[i+1:]...)
			}
			return rule.fault, true
		}
	}
	return Fault{}, false
}

func hijackClose(w http.ResponseWriter) {
	if h, ok := w.(http.Hijacker); ok {
		if conn, _, err := h.Hijack(); err == nil {
			_ = conn.Close()
			return
		}
	}
	panic(http.ErrAbortHandler)
}

// discardWriter processes a request while suppressing its response.
type discardWriter struct{ header http.Header }

func (d *discardWriter) Header() http.Header         { return d.header }
func (d *discardWriter) Write(b []byte) (int, error) { return len(b), nil }
func (d *discardWriter) WriteHeader(int)             {}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, r.Method+" "+r.URL.Path)
	s.mu.Unlock()
	if r.Header.Get("Authorization") != "" {
		// The fake authorizer must never forward credentials; the real one adds them.
		driveError(w, http.StatusBadRequest, "unexpectedCredential", "Test transport forwarded an Authorization header.")
		return
	}
	if fault, ok := s.takeFault(r); ok {
		if fault.Hook != nil {
			fault.Hook(r)
		}
		switch {
		case fault.Status != 0:
			if fault.RetryAfter != "" {
				w.Header().Set("Retry-After", fault.RetryAfter)
			}
			reason := fault.Reason
			if reason == "" {
				reason = "backendError"
			}
			driveError(w, fault.Status, reason, "Injected failure.")
			return
		case fault.DropBeforeCommit:
			hijackClose(w)
			return
		case fault.DropAfterCommit:
			s.handle(&discardWriter{header: http.Header{}}, r)
			hijackClose(w)
			return
		}
	}
	s.handle(w, r)
}

var generatedPattern = regexp.MustCompile(`^/drive/v3/files/generateIds$`)
var byteRange = regexp.MustCompile(`^bytes=(\d+)-(\d+)$`)

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && generatedPattern.MatchString(path):
		s.generateIDs(w, r)
	case r.Method == http.MethodGet && path == "/drive/v3/files":
		s.list(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/drive/v3/files/"):
		s.get(w, r, strings.TrimPrefix(path, "/drive/v3/files/"))
	case r.Method == http.MethodPost && path == "/drive/v3/files":
		s.createFolder(w, r)
	case r.Method == http.MethodPost && path == "/upload/drive/v3/files" && r.URL.Query().Get("uploadType") == "resumable":
		s.startUpload(w, r)
	case r.Method == http.MethodPut && path == "/upload/drive/v3/files" && r.URL.Query().Get("upload_id") != "":
		s.putUpload(w, r)
	default:
		driveError(w, http.StatusNotFound, "notFound", "Unsupported test endpoint.")
	}
}

func (s *Server) generateIDs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	count, err := strconv.Atoi(q.Get("count"))
	if err != nil || count < 1 || count > 1000 || q.Get("space") != "drive" || q.Get("type") != "files" {
		driveError(w, http.StatusBadRequest, "invalid", "Invalid generateIds request.")
		return
	}
	s.mu.Lock()
	ids := make([]string, count)
	for i := range ids {
		ids[i] = s.nextID("gen")
		s.reserved[ids[i]] = true
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"kind": "drive#generatedIds", "space": "drive", "ids": ids})
}

func (s *Server) get(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "root" {
		if !s.RootReadable {
			driveError(w, http.StatusNotFound, "notFound", "File not found: root.")
			return
		}
		id = s.RootID
	}
	o := s.objects[id]
	if !s.visible(o) {
		driveError(w, http.StatusNotFound, "notFound", "File not found: "+id+".")
		return
	}
	if r.URL.Query().Get("alt") == "media" {
		if o.MimeType == FolderMIME {
			driveError(w, http.StatusForbidden, "fileNotDownloadable", "Only files with binary content can be downloaded.")
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		if m := byteRange.FindStringSubmatch(r.Header.Get("Range")); m != nil {
			first, _ := strconv.Atoi(m[1])
			last, _ := strconv.Atoi(m[2])
			if first > last || first >= len(o.Content) {
				driveError(w, http.StatusRequestedRangeNotSatisfiable, "invalidRange", "Range not satisfiable.")
				return
			}
			last = min(last, len(o.Content)-1)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", first, last, len(o.Content)))
			w.Header().Set("Content-Length", strconv.Itoa(last-first+1))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(o.Content[first : last+1])
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(o.Content)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(o.Content)
		return
	}
	writeJSON(w, http.StatusOK, s.resource(o))
}

// validateCreate checks a creation request under the caller-held lock.
func (s *Server) validateCreate(meta createRequest) (int, string) {
	if meta.Name == "" || meta.MimeType == "" || len(meta.Parents) != 1 {
		return http.StatusBadRequest, "invalid"
	}
	if meta.ID != "" {
		if _, exists := s.objects[meta.ID]; exists {
			return http.StatusConflict, "fileIdInUse"
		}
		if !s.reserved[meta.ID] {
			return http.StatusBadRequest, "fileIdNotReserved"
		}
	}
	parent := meta.Parents[0]
	if parent == "root" {
		parent = s.RootID
	}
	p := s.objects[parent]
	if p == nil || (parent != s.RootID && !s.visible(p)) || p.Trashed || p.MimeType != FolderMIME {
		return http.StatusNotFound, "notFound"
	}
	if !p.CanAddChildren {
		return http.StatusForbidden, "insufficientFilePermissions"
	}
	for k, v := range meta.AppProperties {
		if len(k)+len(v) > 124 {
			return http.StatusBadRequest, "invalidAppProperty"
		}
	}
	return 0, ""
}

func (s *Server) create(meta createRequest, content []byte) *Object {
	parent := meta.Parents[0]
	if parent == "root" {
		parent = s.RootID
	}
	id := meta.ID
	if id == "" {
		id = s.nextID("auto")
	}
	delete(s.reserved, id)
	s.version++
	o := &Object{ID: id, Name: meta.Name, MimeType: detectedType(meta.Name, meta.MimeType), Parents: []string{parent}, Content: content, CreatedByApp: true, CanAddChildren: true, Version: s.version}
	if meta.AppProperties != nil {
		o.AppProperties = map[string]string{}
		for k, v := range meta.AppProperties {
			o.AppProperties[k] = v
		}
	}
	s.objects[id] = o
	return o
}

// Like Drive, a binary upload is stored with the media type detected from its
// name (live: README.md became text/markdown).
var detectedTypes = map[string]string{".md": "text/markdown", ".txt": "text/plain", ".js": "text/javascript", ".json": "application/json", ".png": "image/png", ".html": "text/html"}

func detectedType(name, uploaded string) string {
	if uploaded == "application/octet-stream" {
		if i := strings.LastIndexByte(name, '.'); i > 0 {
			if t, ok := detectedTypes[strings.ToLower(name[i:])]; ok {
				return t
			}
		}
	}
	return uploaded
}

func decodeCreate(r *http.Request) (createRequest, bool) {
	var meta createRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return meta, false
	}
	d := json.NewDecoder(strings.NewReader(string(body)))
	d.DisallowUnknownFields()
	return meta, d.Decode(&meta) == nil
}

func (s *Server) createFolder(w http.ResponseWriter, r *http.Request) {
	meta, ok := decodeCreate(r)
	if !ok || meta.MimeType != FolderMIME {
		driveError(w, http.StatusBadRequest, "invalid", "Invalid folder metadata.")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if status, reason := s.validateCreate(meta); status != 0 {
		driveError(w, status, reason, "Folder creation rejected.")
		return
	}
	writeJSON(w, http.StatusOK, s.resource(s.create(meta, nil)))
}

func (s *Server) startUpload(w http.ResponseWriter, r *http.Request) {
	meta, ok := decodeCreate(r)
	size, err := strconv.ParseInt(r.Header.Get("X-Upload-Content-Length"), 10, 64)
	if !ok || err != nil || size < 0 || meta.MimeType == FolderMIME {
		driveError(w, http.StatusBadRequest, "invalid", "Invalid upload metadata.")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if status, reason := s.validateCreate(meta); status != 0 {
		driveError(w, status, reason, "Upload rejected.")
		return
	}
	id := s.nextID("session")
	crd := s.nextID("crd")
	s.sessions[id] = &session{meta: meta, size: size, crd: crd}
	// Like Google, echo the initiation query and append the opaque session
	// parameters upload_id and session_crd (observed in live responses).
	echo := r.URL.Query()
	echo.Set("upload_id", id)
	echo.Set("session_crd", crd)
	w.Header().Set("Location", "https://www.googleapis.com/upload/drive/v3/files?"+echo.Encode())
	w.WriteHeader(http.StatusOK)
}

var rangePattern = regexp.MustCompile(`^bytes (\d+)-(\d+)/(\d+)$`)
var queryPattern = regexp.MustCompile(`^bytes \*/(\d+)$`)

func (s *Server) putUpload(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	if err != nil {
		driveError(w, http.StatusBadRequest, "invalid", "Unreadable chunk.")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[r.URL.Query().Get("upload_id")]
	if sess == nil || r.URL.Query().Get("session_crd") != sess.crd {
		driveError(w, http.StatusNotFound, "notFound", "Upload session not found.")
		return
	}
	if sess.done != nil {
		writeJSON(w, http.StatusOK, s.resource(sess.done))
		return
	}
	header := r.Header.Get("Content-Range")
	if m := queryPattern.FindStringSubmatch(header); m != nil {
		total, _ := strconv.ParseInt(m[1], 10, 64)
		if total != sess.size || len(body) != 0 {
			driveError(w, http.StatusBadRequest, "invalid", "Invalid status query.")
			return
		}
		s.progress(w, sess)
		return
	}
	m := rangePattern.FindStringSubmatch(header)
	if m == nil {
		driveError(w, http.StatusBadRequest, "invalid", "Missing Content-Range.")
		return
	}
	first, _ := strconv.ParseInt(m[1], 10, 64)
	last, _ := strconv.ParseInt(m[2], 10, 64)
	total, _ := strconv.ParseInt(m[3], 10, 64)
	if total != sess.size || last < first || last-first+1 != int64(len(body)) || first > int64(len(sess.received)) {
		driveError(w, http.StatusBadRequest, "invalid", "Invalid chunk range.")
		return
	}
	// Overlapping retransmissions replace uncommitted bytes, as the service does.
	sess.received = append(sess.received[:first], body...)
	s.progress(w, sess)
}

func (s *Server) progress(w http.ResponseWriter, sess *session) {
	if int64(len(sess.received)) == sess.size {
		if status, reason := s.validateCreate(sess.meta); status != 0 {
			driveError(w, status, reason, "Upload completion rejected.")
			return
		}
		sess.done = s.create(sess.meta, append([]byte{}, sess.received...))
		writeJSON(w, http.StatusOK, s.resource(sess.done))
		return
	}
	if len(sess.received) > 0 {
		w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", len(sess.received)-1))
	}
	w.WriteHeader(308)
}

var parentClause = regexp.MustCompile(`'([A-Za-z0-9_-]+)' in parents`)
var propertyClause = regexp.MustCompile(`appProperties has \{ key='([A-Za-z0-9_-]+)' and value='([A-Za-z0-9_-]+)' \}`)
var nameClause = regexp.MustCompile(`name = '((?:[^'\\]|\\.)*)'`)

// list supports the conjunctions LedgeSync issues: parent, trashed, folder MIME
// type, exact name and one appProperties equality. Unsupported text is rejected.
func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := q.Get("q")
	pageSize := 100
	if v := q.Get("pageSize"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1000 {
			driveError(w, http.StatusBadRequest, "invalid", "Invalid pageSize.")
			return
		}
		pageSize = n
	}
	rest := query
	parent := ""
	if m := parentClause.FindStringSubmatch(rest); m != nil {
		parent = m[1]
		rest = strings.Replace(rest, m[0], "", 1)
	}
	property := []string(nil)
	if m := propertyClause.FindStringSubmatch(rest); m != nil {
		property = m[1:]
		rest = strings.Replace(rest, m[0], "", 1)
	}
	name := ""
	hasName := false
	if m := nameClause.FindStringSubmatch(rest); m != nil {
		name = strings.NewReplacer(`\'`, `'`, `\\`, `\`).Replace(m[1])
		hasName = true
		rest = strings.Replace(rest, m[0], "", 1)
	}
	foldersOnly := strings.Contains(rest, "mimeType = '"+FolderMIME+"'")
	rest = strings.Replace(rest, "mimeType = '"+FolderMIME+"'", "", 1)
	trashedFalse := strings.Contains(rest, "trashed = false")
	rest = strings.Replace(rest, "trashed = false", "", 1)
	if strings.Trim(strings.ReplaceAll(rest, "and", ""), " ") != "" {
		driveError(w, http.StatusBadRequest, "invalidQuery", "Unsupported test query.")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if parent == "root" {
		parent = s.RootID
	}
	var matched []*Object
	for _, o := range s.objects {
		if o.ID == s.RootID || !s.visible(o) {
			continue
		}
		if parent != "" && (len(o.Parents) != 1 || o.Parents[0] != parent) {
			continue
		}
		if trashedFalse && o.Trashed || foldersOnly && o.MimeType != FolderMIME || hasName && o.Name != name {
			continue
		}
		if property != nil && o.AppProperties[property[0]] != property[1] {
			continue
		}
		matched = append(matched, o)
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].ID < matched[j].ID })
	start := 0
	if token := q.Get("pageToken"); token != "" {
		n, err := strconv.Atoi(strings.TrimPrefix(token, "page-"))
		if err != nil || n < 0 || n > len(matched) {
			driveError(w, http.StatusBadRequest, "invalid", "Invalid page token.")
			return
		}
		start = n
	}
	end := min(start+pageSize, len(matched))
	files := []map[string]any{}
	for _, o := range matched[start:end] {
		files = append(files, s.resource(o))
	}
	result := map[string]any{"kind": "drive#fileList", "incompleteSearch": false, "files": files}
	if end < len(matched) {
		result["nextPageToken"] = "page-" + strconv.Itoa(end)
	}
	writeJSON(w, http.StatusOK, result)
}

// Package projects manages saved local folder to Google Drive pairs: their
// selection policy, opt-in automatic copies and bounded run history. Planning,
// approval and execution remain in the shared transfer service.
package projects

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alexandroit/LedgeSync/internal/config"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/transfer"
	"github.com/alexandroit/LedgeSync/internal/transferstate"
)

// Project is a saved sync pair. It never stores credentials or approvals that
// could start a manual upload; only an explicit automation authorization can.
type Project struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	SourceRoot  string               `json:"sourceRoot"`
	ConfigPath  string               `json:"configPath,omitempty"`
	Policy      Policy               `json:"policy"`
	Destination transfer.Destination `json:"destination"`
	Automation  Automation           `json:"automation"`
	CreatedAt   string               `json:"createdAt"`
	UpdatedAt   string               `json:"updatedAt"`
	LastRun     *RunSummary          `json:"lastRun,omitempty"`
}

// Policy is the editable selection and safety policy of a project that does not
// use an external configuration file. It maps directly onto config.Config.
type Policy struct {
	Composition    string         `json:"composition"`
	ConflictPolicy string         `json:"conflictPolicy"`
	MaxRetries     int            `json:"maxRetries"`
	Groups         []config.Group `json:"groups"`
}

// Automation is opt-in and bound to an explicit authorization.
type Automation struct {
	Enabled         bool           `json:"enabled"`
	Trigger         string         `json:"trigger"`
	IntervalSeconds int            `json:"intervalSeconds"`
	Authorization   *Authorization `json:"authorization,omitempty"`
	Paused          bool           `json:"paused"`
	PauseCode       string         `json:"pauseCode,omitempty"`
	PauseReason     string         `json:"pauseReason,omitempty"`
	LastCheckedAt   string         `json:"lastCheckedAt,omitempty"`
	LastFingerprint string         `json:"lastFingerprint,omitempty"`
	NextRunAt       string         `json:"nextRunAt,omitempty"`
	Waiting         string         `json:"waiting,omitempty"`
}

// Authorization preauthorizes automatic create-only copies for exactly these
// inputs. Any difference pauses the job; it is never broadened automatically.
type Authorization struct {
	AccountReference string `json:"accountReference"`
	DestinationID    string `json:"destinationId"`
	SourceIdentity   string `json:"sourceIdentity"`
	ConfigDigest     string `json:"configDigest"`
	RulesDigest      string `json:"rulesDigest"`
	ConflictPolicy   string `json:"conflictPolicy"`
	PlanDigest       string `json:"planDigest"`
	ApprovedAt       string `json:"approvedAt"`
}

// RunSummary is the bounded, redacted history record of one run.
type RunSummary struct {
	RunID          string           `json:"runId"`
	ProjectID      string           `json:"projectId"`
	ProjectName    string           `json:"projectName"`
	Destination    string           `json:"destination"`
	Trigger        string           `json:"trigger"`
	State          string           `json:"state"`
	ErrorCode      string           `json:"errorCode,omitempty"`
	Message        string           `json:"message"`
	StartedAt      string           `json:"startedAt"`
	FinishedAt     string           `json:"finishedAt"`
	TotalFiles     int              `json:"totalFiles"`
	CompletedFiles int              `json:"completedFiles"`
	SkippedFiles   int              `json:"skippedFiles"`
	PausedFiles    int              `json:"pausedFiles"`
	TotalBytes     int64            `json:"totalBytes"`
	UploadedBytes  int64            `json:"uploadedBytes"`
	SentBytes      int64            `json:"sentBytes"`
	RemoteFolderID string           `json:"remoteFolderId,omitempty"`
	Issues         []transfer.Issue `json:"issues,omitempty"`
}

const (
	TriggerInterval = "interval"
	TriggerWatch    = "watch"
	// MinimumInterval matches the configuration contract for automation.
	MinimumInterval = 60
	MaximumInterval = 604800
)

// DefaultPolicy is the policy used by "Choose folder": recursive .gitignore,
// conservative composition and keep-both for changed files.
func DefaultPolicy() Policy {
	c := config.Default(string(filepath.Separator) + "default")
	return Policy{Composition: c.Filters.Composition, ConflictPolicy: c.Sync.ConflictPolicy, MaxRetries: c.Sync.MaxRetries, Groups: c.Filters.Groups}
}

// Config builds and validates the full configuration for root.
func (p Policy) Config(root string) (config.Config, error) {
	c := config.Default(root)
	c.Filters.Composition = p.Composition
	c.Filters.Groups = append([]config.Group{}, p.Groups...)
	c.Sync.ConflictPolicy = p.ConflictPolicy
	c.Sync.MaxRetries = p.MaxRetries
	if err := c.Validate(); err != nil {
		return config.Config{}, err
	}
	return c, nil
}

// IsDefault reports whether the policy equals the default "Choose folder" policy.
func (p Policy) IsDefault() bool {
	a, _ := json.Marshal(p)
	b, _ := json.Marshal(DefaultPolicy())
	return string(a) == string(b)
}

// Validate checks the policy against the configuration contract.
func (p Policy) Validate() error {
	_, err := p.Config(string(filepath.Separator) + "validate")
	return err
}

// ValidateAutomation checks schedule settings without enabling anything.
func ValidateAutomation(trigger string, interval int) error {
	if trigger != TriggerInterval && trigger != TriggerWatch {
		return domain.Fail("CONFIG_INVALID", "Choose a periodic check or a change check.")
	}
	if interval < MinimumInterval || interval > MaximumInterval {
		return domain.Fail("CONFIG_INVALID", "Automatic checks run between every minute and every week.")
	}
	return nil
}

// ValidName bounds user-visible project names.
func ValidName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 || !utf8.ValidString(name) || strings.ContainsAny(name, "\x00\r\n") {
		return domain.Fail("CONFIG_INVALID", "Enter a project name of up to 200 characters.")
	}
	return nil
}

func newID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "p" + hex.EncodeToString(b[:])
}

// Store wraps the private catalog with project semantics.
type Store struct {
	dir string
	now func() time.Time
}

func NewStore(dir string) *Store { return &Store{dir: dir, now: time.Now} }

func (s *Store) open() (*transferstate.Catalog, error) { return transferstate.OpenCatalog(s.dir) }

// List returns all saved projects, most recently updated first.
func (s *Store) List() ([]Project, error) {
	c, err := s.open()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	raw, err := c.Projects()
	if err != nil {
		return nil, err
	}
	out := make([]Project, 0, len(raw))
	for _, r := range raw {
		var p Project
		if json.Unmarshal(r, &p) != nil {
			return nil, domain.Fail("STATE_UNAVAILABLE", "A saved project could not be read.")
		}
		out = append(out, p)
	}
	return out, nil
}

// Get returns one project.
func (s *Store) Get(id string) (Project, error) {
	c, err := s.open()
	if err != nil {
		return Project{}, err
	}
	defer c.Close()
	var p Project
	ok, err := c.Project(id, &p)
	if err != nil {
		return Project{}, err
	}
	if !ok {
		return Project{}, domain.Fail("PROJECT_NOT_FOUND", "This sync pair is no longer saved.")
	}
	return p, nil
}

// Save inserts or updates a project after validation.
func (s *Store) Save(p Project) (Project, error) {
	if err := ValidName(p.Name); err != nil {
		return Project{}, err
	}
	if !filepath.IsAbs(p.SourceRoot) {
		return Project{}, domain.Fail("CONFIG_INVALID", "A saved project needs an absolute local folder.")
	}
	if p.ConfigPath == "" {
		if err := p.Policy.Validate(); err != nil {
			return Project{}, err
		}
	}
	if p.Destination.ID == "" || p.Destination.AccountReference == "" {
		return Project{}, domain.Fail("CONFIG_INVALID", "A saved project needs a Google Drive destination.")
	}
	if p.Automation.Enabled {
		if err := ValidateAutomation(p.Automation.Trigger, p.Automation.IntervalSeconds); err != nil {
			return Project{}, err
		}
		if p.Automation.Authorization == nil {
			return Project{}, domain.Fail("AUTOMATION_UNAUTHORIZED", "Automatic copies need an explicit authorization.")
		}
	}
	now := s.now().UTC().Format(time.RFC3339)
	if p.ID == "" {
		p.ID = newID()
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	c, err := s.open()
	if err != nil {
		return Project{}, err
	}
	defer c.Close()
	if err = c.PutProject(p.ID, p); err != nil {
		return Project{}, err
	}
	return p, nil
}

// Delete forgets a project locally. Drive copies are never touched.
func (s *Store) Delete(id string) error {
	c, err := s.open()
	if err != nil {
		return err
	}
	defer c.Close()
	return c.DeleteProject(id)
}

// FindPair returns the saved project for a source, account and destination.
func (s *Store) FindPair(sourceRoot, account, destination string) (Project, bool, error) {
	list, err := s.List()
	if err != nil {
		return Project{}, false, err
	}
	for _, p := range list {
		if samePath(p.SourceRoot, sourceRoot) && p.Destination.AccountReference == account && p.Destination.ID == destination {
			return p, true, nil
		}
	}
	return Project{}, false, nil
}

func samePath(a, b string) bool {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = filepath.Clean(a)
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = filepath.Clean(b)
	}
	return ra == rb
}

// RecordRun stores a summary and updates the project's last run.
func (s *Store) RecordRun(summary RunSummary) error {
	if len(summary.Issues) > transfer.MaxIssues {
		summary.Issues = summary.Issues[:transfer.MaxIssues]
	}
	c, err := s.open()
	if err != nil {
		return err
	}
	defer c.Close()
	finished, err := time.Parse(time.RFC3339, summary.FinishedAt)
	if err != nil {
		finished = s.now()
	}
	if err = c.RecordRun(summary.RunID, summary.ProjectID, finished, summary); err != nil {
		return err
	}
	var p Project
	ok, err := c.Project(summary.ProjectID, &p)
	if err != nil || !ok {
		return err
	}
	last := summary
	last.Issues = nil
	p.LastRun = &last
	return c.PutProject(p.ID, p)
}

// History returns recent runs for one project, or for all when id is empty.
func (s *Store) History(id string, limit int) ([]RunSummary, error) {
	c, err := s.open()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	raw, err := c.Runs(id, limit)
	if err != nil {
		return nil, err
	}
	out := make([]RunSummary, 0, len(raw))
	for _, r := range raw {
		var summary RunSummary
		if json.Unmarshal(r, &summary) != nil {
			return nil, domain.Fail("STATE_UNAVAILABLE", "A saved run summary could not be read.")
		}
		out = append(out, summary)
	}
	return out, nil
}

// ClearHistory removes local run summaries only.
func (s *Store) ClearHistory() error {
	c, err := s.open()
	if err != nil {
		return err
	}
	defer c.Close()
	return c.ClearHistory()
}

// Settings are application preferences stored in the catalog.
type Settings struct {
	DefaultConflictPolicy string `json:"defaultConflictPolicy"`
	DefaultMaxRetries     int    `json:"defaultMaxRetries"`
	AutomationPaused      bool   `json:"automationPaused"`
}

func DefaultSettings() Settings {
	d := DefaultPolicy()
	return Settings{DefaultConflictPolicy: d.ConflictPolicy, DefaultMaxRetries: d.MaxRetries}
}

func (s *Store) Settings() (Settings, error) {
	c, err := s.open()
	if err != nil {
		return Settings{}, err
	}
	defer c.Close()
	out := DefaultSettings()
	if _, err = c.Setting("application", &out); err != nil {
		return Settings{}, err
	}
	return out, nil
}

func (s *Store) SaveSettings(v Settings) (Settings, error) {
	if v.DefaultConflictPolicy != "keep-both" && v.DefaultConflictPolicy != "pause" || v.DefaultMaxRetries < 0 || v.DefaultMaxRetries > 20 {
		return Settings{}, domain.Fail("CONFIG_INVALID", "Choose keep-both or pause, and between 0 and 20 retries.")
	}
	c, err := s.open()
	if err != nil {
		return Settings{}, err
	}
	defer c.Close()
	if err = c.PutSetting("application", v); err != nil {
		return Settings{}, err
	}
	return v, nil
}

// Summarize converts a terminal transfer status into a history record.
func Summarize(p Project, trigger string, st transfer.Status) RunSummary {
	return RunSummary{RunID: st.RunID, ProjectID: p.ID, ProjectName: p.Name, Destination: p.Destination.Name, Trigger: trigger, State: st.State, ErrorCode: st.ErrorCode, Message: st.Message, StartedAt: st.StartedAt, FinishedAt: st.FinishedAt, TotalFiles: st.TotalFiles, CompletedFiles: st.CompletedFiles, SkippedFiles: st.SkippedFiles, PausedFiles: st.PausedFiles, TotalBytes: st.TotalBytes, UploadedBytes: st.UploadedBytes, SentBytes: st.SentBytes, RemoteFolderID: st.RemoteFolderID, Issues: st.Issues}
}

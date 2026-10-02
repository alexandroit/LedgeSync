package projects

import (
	"context"
	"time"

	"github.com/alexandroit/LedgeSync/internal/app"
	"github.com/alexandroit/LedgeSync/internal/config"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
	"github.com/alexandroit/LedgeSync/internal/transfer"
)

// LocalScanner reads a project's source with its policy, without Drive.
type LocalScanner interface {
	Preview(context.Context, string) (app.Preview, error)
	Scan(context.Context, config.Config) (app.Preview, error)
}

// Accounts reports the connected Google account.
type Accounts interface {
	Status(context.Context) (driveauth.Status, error)
}

// ServiceRunner executes authorized automatic checks through the shared
// services. Desktop and CLI use it unchanged; Gate lets the desktop keep
// manual actions and automatic runs mutually exclusive.
type ServiceRunner struct {
	Local    LocalScanner
	Transfer *transfer.Service
	Accounts Accounts
	Store    *Store
	Gate     func() (func(), error)
}

// Source returns the scan input for a saved project.
func Source(p Project) (transfer.Source, error) {
	if p.ConfigPath != "" {
		return transfer.Source{ConfigPath: p.ConfigPath}, nil
	}
	c, err := p.Policy.Config(p.SourceRoot)
	if err != nil {
		return transfer.Source{}, err
	}
	return transfer.Source{Root: p.SourceRoot, Config: &c}, nil
}

func (r ServiceRunner) LocalFingerprint(ctx context.Context, p Project) (string, error) {
	src, err := Source(p)
	if err != nil {
		return "", err
	}
	var preview app.Preview
	if src.Config != nil {
		preview, err = r.Local.Scan(ctx, *src.Config)
	} else {
		preview, err = r.Local.Preview(ctx, src.ConfigPath)
	}
	if err != nil {
		return "", err
	}
	return transfer.Fingerprint(preview)
}

// CheckAuthorization compares a fresh preview with the stored authorization.
// It never broadens an authorization: any difference requires review.
func CheckAuthorization(auth *Authorization, plan transfer.Plan) error {
	switch {
	case auth == nil:
		return domain.Fail("AUTOMATION_UNAUTHORIZED", "Automatic copies are not authorized for this sync pair.")
	case plan.AccountReference != auth.AccountReference:
		return domain.Fail("ACCOUNT_CHANGED", "A different Google account is connected. Automatic copies are paused.")
	case plan.DestinationID != auth.DestinationID:
		return domain.Fail("DESTINATION_CHANGED", "The Drive destination changed. Automatic copies are paused.")
	case plan.SourceIdentity != auth.SourceIdentity:
		return domain.Fail("SOURCE_REPLACED", "The local folder was replaced or is on a different volume. Review and authorize automatic copies again.")
	case plan.ConfigDigest != auth.ConfigDigest || plan.RulesDigest != auth.RulesDigest || plan.ConflictPolicy != auth.ConflictPolicy:
		return domain.Fail("AUTOMATION_REVIEW_REQUIRED", "Ignore rules or the project policy changed since automatic copies were authorized. Preview the sync pair and authorize again.")
	case plan.RecreatedItems > 0:
		return domain.Fail("AUTOMATION_REVIEW_REQUIRED", "Some earlier copies are missing, trashed or moved in Drive. Review a manual preview before automatic copies continue.")
	}
	for _, e := range plan.Entries {
		switch e.Action {
		case transfer.ActionCreate, transfer.ActionUpload, transfer.ActionKeepBoth, transfer.ActionResume, transfer.ActionSkip, transfer.ActionUnsupported, transfer.ActionPaused:
		default:
			return domain.Fail("AUTOMATION_REVIEW_REQUIRED", "The automatic preview contains work that needs your review.")
		}
	}
	return nil
}

// Work counts the entries an automatic run would create or reconcile.
func Work(plan transfer.Plan) int {
	n := 0
	for _, e := range plan.Entries {
		switch e.Action {
		case transfer.ActionCreate, transfer.ActionUpload, transfer.ActionKeepBoth, transfer.ActionResume:
			n++
		}
	}
	return n
}

// NewAuthorization binds automatic copies to a reviewed preview.
func NewAuthorization(plan transfer.Plan, now time.Time) (*Authorization, error) {
	if plan.PlanDigest == "" || plan.SourceIdentity == "" || plan.ConfigDigest == "" || plan.RulesDigest == "" {
		return nil, domain.Fail("PLAN_REQUIRED", "Preview the sync pair, review it, then enable automatic copies.")
	}
	if expires, err := time.Parse(time.RFC3339Nano, plan.ExpiresAt); err != nil || !now.Before(expires) {
		return nil, domain.Fail("PLAN_EXPIRED", "This preview expired. Preview again before enabling automatic copies.")
	}
	if plan.RecreatedItems > 0 {
		return nil, domain.Fail("AUTOMATION_REVIEW_REQUIRED", "Some earlier copies are missing or changed in Drive. Upload this preview manually first, then enable automatic copies.")
	}
	return &Authorization{AccountReference: plan.AccountReference, DestinationID: plan.DestinationID, SourceIdentity: plan.SourceIdentity,
		ConfigDigest: plan.ConfigDigest, RulesDigest: plan.RulesDigest, ConflictPolicy: plan.ConflictPolicy, PlanDigest: plan.PlanDigest, ApprovedAt: now.UTC().Format(time.RFC3339)}, nil
}

func (r ServiceRunner) RunAuthorized(ctx context.Context, p Project) (Outcome, error) {
	if r.Gate != nil {
		release, err := r.Gate()
		if err != nil {
			return Outcome{}, err
		}
		defer release()
	}
	auth := p.Automation.Authorization
	if auth == nil || !p.Automation.Enabled {
		return Outcome{}, domain.Fail("AUTOMATION_UNAUTHORIZED", "Automatic copies are not authorized for this sync pair.")
	}
	status, err := r.Accounts.Status(ctx)
	if err != nil {
		return Outcome{}, err
	}
	if status.State != "connected" || status.Account == nil || status.Account.Reference == "" {
		return Outcome{}, domain.Fail("AUTH_REQUIRED", "Connect Google Drive before automatic copies can continue.")
	}
	if status.Account.Reference != auth.AccountReference {
		return Outcome{}, domain.Fail("ACCOUNT_CHANGED", "A different Google account is connected. Automatic copies are paused.")
	}
	if _, err = r.Transfer.RestoreDestination(ctx, p.Destination); err != nil {
		return Outcome{}, err
	}
	src, err := Source(p)
	if err != nil {
		return Outcome{}, err
	}
	plan, err := r.Transfer.PreviewSource(ctx, src)
	if err != nil {
		return Outcome{}, err
	}
	if err = CheckAuthorization(auth, plan); err != nil {
		r.Transfer.Invalidate()
		return Outcome{}, err
	}
	if Work(plan) == 0 {
		r.Transfer.Invalidate()
		return Outcome{Fingerprint: plan.SourceDigest}, nil
	}
	if _, err = r.Transfer.Start(ctx, plan.PlanDigest); err != nil {
		return Outcome{}, err
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for r.Transfer.Busy() {
		select {
		case <-ctx.Done():
			r.Transfer.CancelAndWait()
		case <-ticker.C:
		}
	}
	summary := Summarize(p, "automatic", r.Transfer.Status())
	if r.Store != nil {
		_ = r.Store.RecordRun(summary)
	}
	return Outcome{Ran: true, Summary: summary, Fingerprint: plan.SourceDigest}, nil
}

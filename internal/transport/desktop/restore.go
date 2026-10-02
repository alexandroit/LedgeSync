package desktop

import (
	"context"

	"github.com/alexandroit/LedgeSync/internal/connections"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/restore"
	"github.com/alexandroit/LedgeSync/internal/transfer"
)

// RestoreProjectCopy downloads a sync pair's verified Drive copy into a new,
// empty folder chosen with the native picker. The source is never written.
func (a *App) RestoreProjectCopy(id string) (*restore.Progress, error) {
	s, err := a.store()
	if err != nil {
		return nil, err
	}
	if a.restoreProvider == nil || a.restorePicker == nil || a.automatic == nil {
		return nil, domain.Fail("RESTORE_UNAVAILABLE", "Restoring copies is unavailable in this build.")
	}
	p, err := s.Get(id)
	if err != nil {
		return nil, connections.PublicError(err)
	}
	if p.SourceIdentity == "" {
		return nil, domain.Fail("RESTORE_UNAVAILABLE", "This sync pair has no recorded copy yet. Upload it once before restoring.")
	}
	a.mu.Lock()
	if a.restoreCancel != nil || a.lifecycle {
		a.mu.Unlock()
		return nil, errTransferBusy
	}
	a.mu.Unlock()
	account, err := a.connectedAccount(a.connectionContext)
	if err != nil {
		return nil, connections.PublicError(err)
	}
	if account != p.Destination.AccountReference {
		return nil, domain.Fail("ACCOUNT_CHANGED", "This copy belongs to a different Google account. Connect that account to restore it.")
	}
	chosen, err := a.restorePicker()
	if err != nil || chosen == "" {
		return nil, connections.PublicError(err)
	}
	stateDir := a.automatic.StateDirectory()
	target, err := restore.PrepareTarget(chosen, []string{p.SourceRoot, stateDir})
	if err != nil {
		return nil, connections.PublicError(err)
	}
	ctx, cancel := context.WithCancel(a.connectionContext)
	runner := &restore.Runner{}
	done := make(chan struct{})
	a.mu.Lock()
	if a.restoreCancel != nil || a.lifecycle {
		a.mu.Unlock()
		cancel()
		return nil, errTransferBusy
	}
	a.restoreCancel, a.restoreRunner, a.restoreDone = cancel, runner, done
	a.mu.Unlock()
	req := restore.Request{StateDir: stateDir, ProjectKey: transfer.ProjectKey(p.SourceIdentity, p.Destination.AccountReference, p.Destination.ID), Account: account, Target: target}
	go func() {
		defer close(done)
		_, _ = runner.Run(ctx, a.restoreProvider, req)
		cancel()
		a.mu.Lock()
		a.restoreCancel = nil
		a.mu.Unlock()
	}()
	progress := runner.Progress()
	return &progress, nil
}

// RestoreStatus reports the current or last restore.
func (a *App) RestoreStatus() *restore.Progress {
	a.mu.Lock()
	runner := a.restoreRunner
	a.mu.Unlock()
	if runner == nil {
		return nil
	}
	p := runner.Progress()
	return &p
}

// CancelRestore stops a running restore. Files already restored remain.
func (a *App) CancelRestore() {
	a.mu.Lock()
	cancel, done := a.restoreCancel, a.restoreDone
	a.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

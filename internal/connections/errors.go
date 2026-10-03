package connections

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
)

// PublicError maps any error to a typed error whose code and message are safe
// to show in the desktop interface or CLI. Unknown errors never pass through:
// operating-system, provider and storage diagnostics can contain private data.
func PublicError(err error) error {
	if err == nil {
		return nil
	}
	var safe *domain.Error
	if errors.As(err, &safe) {
		return domain.Wrap(safe.Code, redactHome(safe.Message), err)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, driveauth.ErrCanceled) {
		return domain.Wrap("CANCELLED", "Operation canceled.", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return domain.Wrap("TIMEOUT", "The operation took too long. Check the connection and try again.", err)
	}
	var callback driveauth.CallbackFailure
	if errors.As(err, &callback) {
		return domain.Wrap("AUTH_CALLBACK_INVALID", callback.Error(), err)
	}
	for _, item := range []struct {
		err  error
		code string
	}{
		{driveauth.ErrBuildConfig, "OAUTH_UNAVAILABLE"},
		{driveauth.ErrSetup, "OAUTH_UNAVAILABLE"},
		{driveauth.ErrStorage, "AUTH_STORAGE_UNAVAILABLE"},
		{driveauth.ErrRevokedCleanup, "AUTH_STORAGE_UNAVAILABLE"},
		{driveauth.ErrBusy, "AUTH_BUSY"},
		{driveauth.ErrReconnect, "AUTH_REQUIRED"},
		{driveauth.ErrNotFound, "AUTH_REQUIRED"},
		{driveauth.ErrClientChanged, "AUTH_CLIENT_CHANGED"},
		{driveauth.ErrIdentity, "AUTH_IDENTITY_CHANGED"},
		{driveauth.ErrScope, "AUTH_SCOPE_REQUIRED"},
		{driveauth.ErrScopeNotGranted, "AUTH_SCOPE_NOT_GRANTED"},
		{driveauth.ErrScopeUnexpected, "AUTH_SCOPE_UNEXPECTED"},
		{driveauth.ErrNoFolderSelected, "DRIVE_FOLDER_NOT_SELECTED"},
		{driveauth.ErrDenied, "AUTH_DENIED"},
		{driveauth.ErrTimeout, "AUTH_TIMEOUT"},
		{driveauth.ErrConnected, "AUTH_CONNECTED"},
		{driveauth.ErrBrowser, "AUTH_BROWSER_UNAVAILABLE"},
		{driveauth.ErrCallback, "AUTH_CALLBACK_INVALID"},
		{driveauth.ErrNetwork, "DRIVE_NETWORK"},
		{driveauth.ErrToken, "AUTH_INVALID_RESPONSE"},
		{driveauth.ErrProvider, "DRIVE_REQUEST_FAILED"},
		{driveauth.ErrRevokeConfirmation, "REVOKE_CONFIRMATION_REQUIRED"},
		{driveauth.ErrRevokeFailed, "REVOKE_UNCONFIRMED"},
		{driveauth.ErrManagedClient, "OAUTH_MANAGED"},
		{driveauth.ErrClient, "OAUTH_CLIENT_INVALID"},
	} {
		if errors.Is(err, item.err) {
			return domain.Wrap(item.code, item.err.Error(), err)
		}
	}
	return domain.Wrap("INTERNAL_ERROR", "LedgeSync could not complete this action. Try again; if it continues, check the connection and the system credential vault.", err)
}

// redactHome removes the user's home directory from messages that may carry a
// path. Source-relative paths remain, because they identify the affected item.
func redactHome(message string) string {
	home, err := os.UserHomeDir()
	if err != nil || len(home) < 2 {
		return message
	}
	return strings.ReplaceAll(message, home, "~")
}

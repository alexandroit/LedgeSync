package connections

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/alexandroit/LedgeSync/internal/config"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
)

// ValidateSourceSelection checks before the first OAuth vault operation: creating its protected lock
// must not mutate a selected source that contains the application settings.
func ValidateSourceSelection(source string, isConfig bool) error {
	settingsBase, err := os.UserConfigDir()
	if err != nil {
		return driveauth.ErrStorage
	}
	return validateSourceSelection(source, isConfig, filepath.Join(settingsBase, "LedgeSync"))
}

func validateSourceSelection(source string, isConfig bool, settings string) error {
	if isConfig {
		cfg, err := config.Load(source)
		if err != nil {
			return err
		}
		source = cfg.Source.Root
	}
	root, err := filepath.EvalSymlinks(source)
	if err != nil {
		return domain.Fail("SOURCE_UNAVAILABLE", "The source directory is unavailable.")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return domain.Fail("SOURCE_UNAVAILABLE", "The source directory is unavailable.")
	}
	sourceInfo, err := os.Stat(root)
	if err != nil || !sourceInfo.IsDir() {
		return domain.Fail("SOURCE_UNAVAILABLE", "Choose an existing source directory.")
	}
	// Resolve the nearest existing settings ancestor, without creating anything.
	existing := settings
	for {
		_, err = os.Stat(existing)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) || filepath.Dir(existing) == existing {
			return driveauth.ErrStorage
		}
		existing = filepath.Dir(existing)
	}
	existing, err = filepath.EvalSymlinks(existing)
	if err != nil {
		return driveauth.ErrStorage
	}
	for path := existing; ; path = filepath.Dir(path) {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return driveauth.ErrStorage
		}
		if os.SameFile(sourceInfo, info) {
			return domain.Fail("STATE_INSIDE_SOURCE", "Choose a source outside the LedgeSync settings directory and its parents.")
		}
		if filepath.Dir(path) == path {
			break
		}
	}
	if settingsInfo, statErr := os.Stat(settings); statErr == nil {
		for path := root; ; path = filepath.Dir(path) {
			info, err := os.Stat(path)
			if err != nil {
				return domain.Fail("SOURCE_UNAVAILABLE", "The source directory changed.")
			}
			if os.SameFile(settingsInfo, info) {
				return domain.Fail("STATE_INSIDE_SOURCE", "LedgeSync settings cannot be used as an upload source.")
			}
			if filepath.Dir(path) == path {
				break
			}
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return driveauth.ErrStorage
	}
	return nil
}

//go:build (!darwin && !windows && !linux) || (darwin && !cgo)

package credentialvault

type unavailableStore struct{}

func newNativeStore() Store                         { return unavailableStore{} }
func (unavailableStore) Get(string) (string, error) { return "", ErrUnavailable }
func (unavailableStore) Set(string, string) error   { return ErrUnavailable }
func (unavailableStore) Delete(string) error        { return ErrUnavailable }

//go:build darwin && cgo

package credentialvault

import (
	"errors"
	keychain "github.com/keybase/go-keychain"
)

type macStore struct{}

func newNativeStore() Store { return macStore{} }

func macQuery(key string) keychain.Item {
	item := keychain.NewItem()
	item.SetSecClass(keychain.SecClassGenericPassword)
	item.SetService(serviceName)
	item.SetAccount(key)
	item.SetSynchronizable(keychain.SynchronizableNo)
	return item
}

func macError(err error) error {
	if errors.Is(err, keychain.ErrorItemNotFound) {
		return ErrNotFound
	}
	return normalizeError(err)
}

func (macStore) Get(key string) (string, error) {
	query := macQuery(key)
	query.SetMatchLimit(keychain.MatchLimitOne)
	query.SetReturnData(true)
	results, err := keychain.QueryItem(query)
	if err != nil {
		return "", macError(err)
	}
	if len(results) == 0 {
		return "", ErrNotFound
	}
	if len(results) != 1 {
		return "", ErrUnavailable
	}
	defer clear(results[0].Data)
	return string(results[0].Data), nil
}

func (macStore) Set(key, value string) error {
	item := macQuery(key)
	data := []byte(value)
	defer clear(data)
	item.SetLabel("LedgeSync Google Drive authorization")
	item.SetData(data)
	item.SetAccessible(keychain.AccessibleWhenUnlockedThisDeviceOnly)
	err := keychain.AddItem(item)
	if errors.Is(err, keychain.ErrorDuplicateItem) {
		// Update atomically; never delete a working refresh token before writing.
		update := keychain.NewItem()
		update.SetData(data)
		err = keychain.UpdateItem(macQuery(key), update)
	}
	return macError(err)
}

func (macStore) Delete(key string) error { return macError(keychain.DeleteItem(macQuery(key))) }

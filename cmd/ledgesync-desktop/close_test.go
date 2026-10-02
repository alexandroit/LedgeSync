//go:build desktop || bindings

package main

import (
	"errors"
	"testing"
)

func TestNativeCloseKeepsActiveUploadUntilExplicitConfirmation(t *testing.T) {
	for _, choice := range []string{"", "Keep Open", "Cancel", "unexpected"} {
		if !preventClose(true, func() (string, error) { return choice, nil }, func() { t.Fatal("unconfirmed close stopped upload") }) {
			t.Fatal("unconfirmed choice allowed close")
		}
	}
	if !preventClose(true, func() (string, error) { return "Stop and Close", errors.New("native dialog unavailable") }, func() { t.Fatal("dialog failure stopped upload") }) {
		t.Fatal("native dialog failure allowed close")
	}
	drained := false
	if preventClose(true, func() (string, error) { return "Stop and Close", nil }, func() { drained = true }) || !drained {
		t.Fatal("confirmed close did not drain upload before returning")
	}
	if preventClose(false, func() (string, error) { t.Fatal("idle app prompted about upload"); return "", nil }, func() { t.Fatal("idle close started upload cleanup") }) {
		t.Fatal("idle app could not close")
	}
}

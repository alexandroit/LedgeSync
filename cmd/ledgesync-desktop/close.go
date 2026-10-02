//go:build desktop || bindings

package main

// preventClose defaults to keeping an active transfer open. The native shell
// callback returns only after an explicitly confirmed shutdown has drained it.
func preventClose(uploadActive bool, prompt func() (string, error), shutdown func()) bool {
	if !uploadActive {
		return false
	}
	answer, err := prompt()
	if err != nil || answer != "Stop and Close" {
		return true
	}
	shutdown()
	return false
}

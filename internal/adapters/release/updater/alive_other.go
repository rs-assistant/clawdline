//go:build windows

package updater

// processAlive answers false where no app bundle is installed, so every
// poll lists processes as before.
func processAlive(int) bool { return false }

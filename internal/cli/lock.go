package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

// acquireFileLock creates a lock file and acquires an exclusive advisory lock.
// Uses gofrs/flock for cross-platform support (flock on Unix, LockFileEx on Windows).
// Returns an unlock function that releases the lock and removes the file.
// The lock is automatically released if the process exits.
func acquireFileLock(path string) (unlock func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	fl := flock.New(path)
	if err := fl.Lock(); err != nil {
		return nil, fmt.Errorf("acquiring lock: %w", err)
	}

	return func() {
		_ = fl.Unlock()
		// Don't remove the lock file — it's harmless on disk and removing it
		// can race with another process that just acquired the lock.
	}, nil
}

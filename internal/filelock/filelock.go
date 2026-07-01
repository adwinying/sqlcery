package filelock

import "github.com/gofrs/flock"

// Lock owns an exclusive advisory lock until Release is called or the process exits.
type Lock struct {
	flock *flock.Flock
}

// Acquire waits for exclusive ownership of path.
func Acquire(path string) (*Lock, error) {
	fileLock := flock.New(path)
	if err := fileLock.Lock(); err != nil {
		return nil, err
	}
	return &Lock{flock: fileLock}, nil
}

// Release relinquishes ownership and closes the lock file descriptor.
func (l *Lock) Release() error {
	if l == nil || l.flock == nil {
		return nil
	}
	if err := l.flock.Unlock(); err != nil {
		return err
	}
	l.flock = nil
	return nil
}

package server

import (
	"context"
)

type imageLock struct {
	held chan struct{}
	refs int
}

// References include holders and waiters, so a key cannot be removed while
// another caller still uses it. Entries disappear once the operation finishes.
func (a *App) lockImage(ctx context.Context, id string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.imageLocksMu.Lock()
	if a.imageLocks == nil {
		a.imageLocks = make(map[string]*imageLock)
	}
	lock := a.imageLocks[id]
	if lock == nil {
		lock = &imageLock{held: make(chan struct{}, 1)}
		a.imageLocks[id] = lock
	}
	lock.refs++
	a.imageLocksMu.Unlock()
	unref := func() {
		a.imageLocksMu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(a.imageLocks, id)
		}
		a.imageLocksMu.Unlock()
	}
	select {
	case lock.held <- struct{}{}:
		release := func() { <-lock.held; unref() }
		if err := ctx.Err(); err != nil {
			release()
			return nil, err
		}
		return release, nil
	case <-ctx.Done():
		unref()
		return nil, ctx.Err()
	}
}

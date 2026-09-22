package parking

import "sync"

type lockEntry struct {
	mu   sync.Mutex
	refs int
}
type LockManager struct {
	mu    sync.Mutex
	locks map[string]*lockEntry
}

func NewLockManager() *LockManager { return &LockManager{locks: make(map[string]*lockEntry)} }

func (m *LockManager) Lock(key string) func() {
	m.mu.Lock()
	e := m.locks[key]
	if e == nil {
		e = &lockEntry{}
		m.locks[key] = e
	}
	e.refs++
	m.mu.Unlock()
	e.mu.Lock()
	return func() {
		e.mu.Unlock()
		m.mu.Lock()
		e.refs--
		if e.refs == 0 {
			delete(m.locks, key)
		}
		m.mu.Unlock()
	}
}

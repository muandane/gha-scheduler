package dispatch

import "sync"

type jobMutexRegistry struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func newJobMutexRegistry() *jobMutexRegistry {
	return &jobMutexRegistry{locks: make(map[string]*sync.Mutex)}
}

func (r *jobMutexRegistry) acquire(jobID string) func() {
	r.mu.Lock()
	m, ok := r.locks[jobID]
	if !ok {
		m = &sync.Mutex{}
		r.locks[jobID] = m
	}
	r.mu.Unlock()
	m.Lock()
	return m.Unlock
}

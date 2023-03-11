package event

import "sync"

type SyncMap[K comparable, V any] struct {
	m    map[K]V
	lock sync.RWMutex
}

func NewSyncMap[K comparable, V any]() *SyncMap[K, V] {
	return &SyncMap[K, V]{m: map[K]V{}}
}

func (m *SyncMap[K, V]) Load(k K) (V, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	v, ok := m.m[k]
	return v, ok
}

func (m *SyncMap[K, V]) Store(k K, v V) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.m[k] = v
}

func (m *SyncMap[K, V]) Delete(k K) V {
	m.lock.Lock()
	defer m.lock.Unlock()
	v := m.m[k]
	delete(m.m, k)
	return v
}

func (m *SyncMap[K, V]) Clear() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.m = map[K]V{}
}

func (m *SyncMap[K, V]) Len() int {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return len(m.m)
}

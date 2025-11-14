package controller

import "sync"

type SafeMap struct {
	data map[string]*sync.Mutex
	mu   sync.Mutex
}

func NewSafeMap() *SafeMap {
	return &SafeMap{
		data: make(map[string]*sync.Mutex),
	}
}

func (sm *SafeMap) DeleteMutex(key string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.data, key)
}

func (sm *SafeMap) AddMutex(key string) *sync.Mutex {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.data[key] = &sync.Mutex{}
	return sm.data[key]
}

func (sm *SafeMap) GetMutex(key string) *sync.Mutex {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, ok := sm.data[key]; !ok {
		sm.data[key] = &sync.Mutex{}
	}
	return sm.data[key]
}

func (sm *SafeMap) DeleteUnknownKeys(knownKeys []string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	knownKeysMap := make(map[string]bool)
	for _, key := range knownKeys {
		knownKeysMap[key] = true
	}

	for key := range sm.data {
		if !knownKeysMap[key] {
			delete(sm.data, key)
		}
	}
}

// Modify safe map to use generics on both key and value types
type GenericSafeMap[K comparable, V any] struct {
	data map[K]V
	mu   sync.Mutex
}

func NewGenericSafeMap[K comparable, V any]() *GenericSafeMap[K, V] {
	return &GenericSafeMap[K, V]{
		data: make(map[K]V),
	}
}

func (gsm *GenericSafeMap[K, V]) Delete(key K) {
	gsm.mu.Lock()
	defer gsm.mu.Unlock()

	delete(gsm.data, key)
}

func (gsm *GenericSafeMap[K, V]) Add(key K, value V) {
	gsm.mu.Lock()
	defer gsm.mu.Unlock()

	gsm.data[key] = value
}

func (gsm *GenericSafeMap[K, V]) Get(key K) (V, bool) {
	gsm.mu.Lock()
	defer gsm.mu.Unlock()

	value, ok := gsm.data[key]
	return value, ok
}

func (gsm *GenericSafeMap[K, V]) DeleteUnknownKeys(knownKeys []K) {
	gsm.mu.Lock()
	defer gsm.mu.Unlock()

	knownKeysMap := make(map[K]bool)
	for _, key := range knownKeys {
		knownKeysMap[key] = true
	}

	for key := range gsm.data {
		if !knownKeysMap[key] {
			delete(gsm.data, key)
		}
	}
}

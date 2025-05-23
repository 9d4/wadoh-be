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

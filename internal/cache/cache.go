package cache

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

const TTL = 5 * time.Minute

type entry struct {
	data   []byte
	expiry time.Time
}

var (
	mu    sync.RWMutex
	store = make(map[string]entry)
)

func init() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		for range ticker.C {
			mu.Lock()
			now := time.Now()
			for k, v := range store {
				if now.After(v.expiry) {
					delete(store, k)
				}
			}
			mu.Unlock()
		}
	}()
}

func Get(_ context.Context, key string, dest any) bool {
	mu.RLock()
	e, ok := store[key]
	mu.RUnlock()
	if !ok || time.Now().After(e.expiry) {
		return false
	}
	return json.Unmarshal(e.data, dest) == nil
}

func Set(_ context.Context, key string, value any, ttl time.Duration) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	mu.Lock()
	store[key] = entry{data: data, expiry: time.Now().Add(ttl)}
	mu.Unlock()
}

func InvalidateByPrefix(_ context.Context, prefix string) {
	mu.Lock()
	for k := range store {
		if strings.HasPrefix(k, prefix) {
			delete(store, k)
		}
	}
	mu.Unlock()
}

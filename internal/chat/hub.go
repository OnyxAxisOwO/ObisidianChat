package chat

import (
	"encoding/json"
	"sync"
	"time"
)

type subscription struct {
	events chan []byte
	done   chan struct{}
}
type Hub struct {
	mu      sync.Mutex
	users   map[string]map[*subscription]bool
	count   int
	dropped uint64
}

func newHub() *Hub { return &Hub{users: make(map[string]map[*subscription]bool)} }
func (h *Hub) subscribe(user string) (*subscription, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.users[user]) >= 4 || h.count >= 10000 {
		return nil, false
	}
	sub := &subscription{make(chan []byte, 32), make(chan struct{})}
	if h.users[user] == nil {
		h.users[user] = make(map[*subscription]bool)
	}
	h.users[user][sub] = true
	h.count++
	return sub, true
}
func (h *Hub) removeLocked(user string, sub *subscription) {
	if h.users[user][sub] {
		delete(h.users[user], sub)
		h.count--
		close(sub.done)
		if len(h.users[user]) == 0 {
			delete(h.users, user)
		}
	}
}
func (h *Hub) unsubscribe(user string, sub *subscription) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.removeLocked(user, sub)
}
func (h *Hub) disconnect(user string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.users[user] {
		h.removeLocked(user, sub)
	}
}
func (h *Hub) online(user string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.users[user]) > 0
}
func (h *Hub) stats() (int, int, uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.count, len(h.users), h.dropped
}
func (h *Hub) publish(users []string, event any) {
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, user := range users {
		for sub := range h.users[user] {
			select {
			case sub.events <- payload:
			default:
				h.dropped++
				h.removeLocked(user, sub)
			}
		}
	}
}

type bucket struct {
	tokens  float64
	updated time.Time
}
type limiter struct {
	mu    sync.Mutex
	items map[string]bucket
}

func (l *limiter) allow(key string, burst int, perSecond float64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := time.Now()
	if l.items == nil {
		l.items = make(map[string]bucket)
	}
	b, ok := l.items[key]
	if !ok {
		if len(l.items) >= 20000 {
			for k, v := range l.items {
				if t.Sub(v.updated) > 10*time.Minute {
					delete(l.items, k)
				}
			}
			if len(l.items) >= 20000 {
				return false
			}
		}
		b = bucket{float64(burst), t}
	}
	b.tokens = min(float64(burst), b.tokens+t.Sub(b.updated).Seconds()*perSecond)
	b.updated = t
	allowed := b.tokens >= 1
	if allowed {
		b.tokens--
	}
	l.items[key] = b
	return allowed
}

package logstream

import (
	// "context"
	"sync"
)

type Event struct {
	JobID  int64
	Content string
}

type Hub struct {
	subscribers map[int64]map[chan Event]struct{} // jobID -> set of subscriber chans
	mu          sync.Mutex
}

func NewHub() *Hub {
	return &Hub{
		subscribers: make(map[int64]map[chan Event]struct{})}
}

func (h *Hub) Subscribe(jobID int64, buffer int) (<-chan Event, func()) {
	ch := make(chan Event, buffer)

	h.mu.Lock()
	if h.subscribers[jobID] == nil {
		h.subscribers[jobID] = make(map[chan Event]struct{})
	}
	h.subscribers[jobID][ch] = struct{}{}
	h.mu.Unlock()

	unsubscribe := func() {
		h.mu.Lock()
		if m := h.subscribers[jobID]; m != nil {
			if _, ok := m[ch]; ok {
				delete(m, ch)
				close(ch)
			}
			if len(m) == 0 {
				delete(h.subscribers, jobID)
			}
	}
	h.mu.Unlock()
	}
	return ch, unsubscribe
}

func (h *Hub) Publish(jobID int64, event Event) {
	h.mu.Lock()
	m := h.subscribers[jobID]
	h.mu.Unlock()

	if len(m) == 0 {
		return 
	}

	ev := Event{
		JobID:  jobID,
		Content: event.Content,
	}

	for ch := range m {
		// non-blocking publish (drop if slow client)
		select {
		case ch <- ev:
		default:
			// drop event if subscriber channel is full
		}
	}


}
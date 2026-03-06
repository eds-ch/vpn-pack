package state

import (
	"sync"
	"time"
)

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Source    string `json:"source,omitempty"`
}

func NewLogEntry(level, message, source string) LogEntry {
	return LogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     level,
		Message:   message,
		Source:    source,
	}
}

type LogBuffer struct {
	mu      sync.Mutex
	entries []LogEntry
	head    int
	count   int
	maxSize int
}

func NewLogBuffer(maxSize int) *LogBuffer {
	return &LogBuffer{
		entries: make([]LogEntry, maxSize),
		maxSize: maxSize,
	}
}

func (lb *LogBuffer) Add(e LogEntry) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.entries[lb.head] = e
	lb.head = (lb.head + 1) % lb.maxSize
	if lb.count < lb.maxSize {
		lb.count++
	}
}

func (lb *LogBuffer) Snapshot() []LogEntry {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	out := make([]LogEntry, lb.count)
	for i := range lb.count {
		idx := (lb.head - 1 - i + lb.maxSize) % lb.maxSize
		out[i] = lb.entries[idx]
	}
	return out
}

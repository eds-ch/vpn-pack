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

// minLogBufferSize is the floor NewLogBuffer enforces so the ring math
// (modulo maxSize) is always defined and Add never indexes a zero slice.
// BUG-L7: callers that pass zero or a negative value used to crash the
// process; clamp so the buffer is always at least usable.
const minLogBufferSize = 1

func NewLogBuffer(maxSize int) *LogBuffer {
	if maxSize < minLogBufferSize {
		maxSize = minLogBufferSize
	}
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

package kbmatch

import "sync"

// Term is a single weighted term of a knowledge base entry.
type Term struct {
	Text   string
	Weight int
}

// Entry is a knowledge base entry.
type Entry struct {
	ID        string
	Answer    string
	Threshold string
	Terms     []Term
}

// RowError describes a single invalid row in an Import batch.
type RowError struct {
	Line   int
	Field  string
	Reason string
}

// Result is the outcome of a successful Match.
type Result struct {
	ID          string
	HitWeight   int
	TotalWeight int
}

// Engine is an in-memory knowledge base matching engine.
type Engine struct {
	mu    sync.RWMutex
	byID  map[string]*entry
	bySeq []*entry
	lists map[string]*postingList
}

// New creates an empty Engine.
func New() *Engine {
	return &Engine{
		byID:  make(map[string]*entry),
		lists: make(map[string]*postingList),
	}
}

// Match finds the best scoring entry at or above its threshold.
func (e *Engine) Match(text string) (Result, bool, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.matchLocked(text)
}

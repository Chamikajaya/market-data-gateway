package book

import (
	"sync"

	"market-gw.com/internal/domain"
)

// Registry holds the order books for configured symbols.
type Registry struct {
	mu    sync.Mutex
	books map[domain.Symbol]*Book // symbol:book
}

func NewRegistry() *Registry {
	return &Registry{
		books: make(map[domain.Symbol]*Book),
	}
}

func (r *Registry) GetOrCreate(symbol domain.Symbol) *Book {
	r.mu.Lock()
	defer r.mu.Unlock()

	if b, ok := r.books[symbol]; ok {
		return b
	}

	b := NewBook(symbol)
	r.books[symbol] = b

	return b
}

func (r *Registry) Get(symbol domain.Symbol) (*Book, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	b, ok := r.books[symbol]
	return b, ok
}

func (r *Registry) Symbols() []domain.Symbol {
	r.mu.Lock()
	defer r.mu.Unlock()

	symbols := make([]domain.Symbol, 0, len(r.books))
	// iterating over keys
	for sym := range r.books {
		symbols = append(symbols, sym)
	}

	return symbols
}

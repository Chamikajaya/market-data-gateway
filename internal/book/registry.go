package book

import (
	"sync"

	"market-gw.com/internal/domain"
)

// holds order books keyed by BookKey (exchange:symbol)
type Registry struct {
	mu    sync.Mutex
	books map[domain.BookKey]*Book
}

func NewRegistry() *Registry {
	return &Registry{
		books: make(map[domain.BookKey]*Book),
	}
}

func (r *Registry) GetOrCreate(exchange string, symbol domain.Symbol) *Book {
	key := domain.MakeBookKey(exchange, symbol)

	r.mu.Lock()
	defer r.mu.Unlock()

	if b, ok := r.books[key]; ok {
		return b
	}

	b := NewBook(exchange, symbol)
	r.books[key] = b
	return b
}

func (r *Registry) Get(key domain.BookKey) (*Book, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	b, ok := r.books[key]
	return b, ok
}

func (r *Registry) Keys() []domain.BookKey {
	r.mu.Lock()
	defer r.mu.Unlock()

	keys := make([]domain.BookKey, 0, len(r.books))
	for k := range r.books {
		keys = append(keys, k)
	}
	return keys
}

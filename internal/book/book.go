package book

import (
	"strconv"
	"sync"
	"time"

	"market-gw.com/internal/domain"
)

// maintains per-exchange, per-symbol order book state.
type Book struct {
	mu       sync.RWMutex
	exchange string
	symbol   domain.Symbol

	bids map[string]string // price → quantity
	asks map[string]string // price → quantity

	lastUpdated time.Time
}

func NewBook(exchange string, symbol domain.Symbol) *Book {
	return &Book{
		exchange: exchange,
		symbol:   symbol,
		bids:     make(map[string]string),
		asks:     make(map[string]string),
	}
}

func (b *Book) Apply(u domain.Update) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch u.Type {
	case domain.UpdateTypeSnapshot:
		b.bids = make(map[string]string, len(u.Bids))
		b.asks = make(map[string]string, len(u.Asks))

		for _, lvl := range u.Bids {
			b.bids[lvl.Price] = lvl.Quantity
		}
		for _, lvl := range u.Asks {
			b.asks[lvl.Price] = lvl.Quantity
		}

	case domain.UpdateTypeDelta:
		applyLevels(b.bids, u.Bids)
		applyLevels(b.asks, u.Asks)
	}

	b.lastUpdated = u.ReceivedAt
}

// returns an immutable copy of the current order book state.
func (b *Book) Snapshot() domain.OrderBook {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return domain.OrderBook{
		Exchange:      b.exchange,
		Symbol:        b.symbol,
		Bids:          toLevels(b.bids),
		Asks:          toLevels(b.asks),
		LastUpdatedAt: b.lastUpdated,
	}
}

// converts the internal map into a slice of Levels
func toLevels(m map[string]string) []domain.Level {
	if len(m) == 0 {
		return nil
	}
	levels := make([]domain.Level, 0, len(m))
	for price, qty := range m {
		levels = append(levels, domain.Level{Price: price, Quantity: qty})
	}
	return levels
}

func applyLevels(m map[string]string, levels []domain.Level) {
	for _, lvl := range levels {
		if isZero(lvl.Quantity) {
			delete(m, lvl.Price)
		} else {
			m[lvl.Price] = lvl.Quantity
		}
	}
}

func isZero(qty string) bool {
	f, err := strconv.ParseFloat(qty, 64)
	if err != nil {
		return false
	}
	return f == 0
}

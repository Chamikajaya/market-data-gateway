// maintains per symbol order book state (btc / eth ...) + applies incremental delta updates and produces snapshots for downstream clients

package book

import (
	"sort"
	"strconv"
	"sync"
	"time"

	"market-gw.com/internal/domain"
)

type Book struct {
	mu     sync.RWMutex // to avoid race conditions between pipeline writes & ws server reads - multiple go routines to read using RLock() + pipeline when writing Lock()
	symbol domain.Symbol

	bids map[string]string
	asks map[string]string

	lastUpdated time.Time
}

func NewBook(symbol domain.Symbol) *Book {

	return &Book{
		symbol: symbol,
		// maps:- instant lookups for delta updates
		bids: make(map[string]string), // price:quantity
		asks: make(map[string]string), // price: quantity
	}
}

func (b *Book) Apply(u domain.Update) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch u.Type {

	case domain.UpdateTypeSnapshot:
		b.bids = make(map[string]string, len(u.Bids))
		b.asks = make(map[string]string, len(u.Asks))

		for _, level := range u.Bids {
			b.bids[level.Price] = level.Quantity
		}

		for _, level := range u.Asks {
			b.asks[level.Price] = level.Quantity
		}

	case domain.UpdateTypeDelta:
		applyLevels(b.bids, u.Bids)
		applyLevels(b.asks, u.Asks)
	}

	b.lastUpdated = u.ReceivedAt

}

func isQuantityZero(quantity string) bool {
	f, err := strconv.ParseFloat(quantity, 64)
	if err != nil {
		return false
	}
	return f == 0
}

func applyLevels(m map[string]string, levels []domain.Level) {

	for _, level := range levels {
		// if the quantity is zero -> delete from map
		if isQuantityZero(level.Quantity) {
			delete(m, level.Price)
		} else {
			m[level.Price] = level.Quantity
		}

	}
}

func (b *Book) BookSnapshot() domain.OrderBook {
	// used by many go routines
	// to get an immutable copy of the current orderbook state
	b.mu.RLock()
	defer b.mu.Unlock()

	return domain.OrderBook{
		Symbol:        b.symbol,
		Bids:          sortLevels(b.bids, true),
		Asks:          sortLevels(b.asks, false),
		LastUpdatedAt: b.lastUpdated,
	}

}

func sortLevels(m map[string]string, isDescending bool) []domain.Level {
	if (len(m)) == 0 {
		return nil
	}

	sortedLevels := make([]domain.Level, 0, len(m))
	// map to slice
	for price, quantity := range m {
		sortedLevels = append(sortedLevels, domain.Level{Price: price, Quantity: quantity})
	}

	sort.Slice(sortedLevels, func(i, j int) bool {

		// ! TODO: Ignoring the error here -> need to handle the error parsing at adapter level
		price_i, _ := strconv.ParseFloat(sortedLevels[i].Price, 64)
		price_j, _ := strconv.ParseFloat(sortedLevels[j].Price, 64)

		if isDescending {
			return price_i > price_j
		}

		return price_i < price_j
	})

	return sortedLevels
}

func (b *Book) Symbol() domain.Symbol {
	return b.symbol
}

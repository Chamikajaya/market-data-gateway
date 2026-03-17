// maintains per symbol order book state (btc / eth ...) + applies incremental delta updates and produces snapshots for downstream clients

package book

import (
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
		// TODO
	}

	b.lastUpdated = u.ReceivedAt

}

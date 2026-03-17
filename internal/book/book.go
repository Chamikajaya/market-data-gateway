// maintains per symbol order book state (btc / eth ...) + applies incremental delta updates and produces snapshots for downstream clients

package book

import (
	"sync"
	"time"

	"market-gw.com/internal/domain"
)

type Book struct {
	mu     sync.RWMutex // ! to avoid race conditions between pipeline writes & ws server reads
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

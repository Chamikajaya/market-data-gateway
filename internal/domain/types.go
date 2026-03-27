package domain

import "time"

type Symbol string

// uniquely identifies an order book: "exchange:symbol".
type BookKey string

func MakeBookKey(exchange string, symbol Symbol) BookKey {
	return BookKey(exchange + ":" + string(symbol))
}

// single price point in the order book.
type Level struct {
	Price    string `json:"price"`
	Quantity string `json:"qty"`
}

type UpdateType int8

const (
	UpdateTypeSnapshot UpdateType = iota
	UpdateTypeDelta
)

type Update struct {
	Exchange   string
	Symbol     Symbol
	Type       UpdateType
	Bids       []Level
	Asks       []Level
	ReceivedAt time.Time
}

// current state of one book, ready to send to clients.
type OrderBook struct {
	Exchange      string  `json:"exchange"`
	Symbol        Symbol  `json:"symbol"`
	Bids          []Level `json:"bids"`
	Asks          []Level `json:"asks"`
	LastUpdatedAt time.Time
}

package domain

import "time"

// ticker
type Symbol string

type Level struct {
	// used string to avoid floating point errors - consumers have to parse to decimal
	Price    string
	Quantity string
}

type UpdateType int8

const (
	// adapter has fetched a full order book via REST
	UpdateTypeSnapshot UpdateType = iota
	// adapter received a ws delta message
	UpdateTypeDelta
)

// this is what every exchange must produce and what downstream stage (book/ pipeline / server ) consumes
type Update struct {
	Exchange   string
	Symbol     Symbol
	Type       UpdateType
	Bids       []Level
	Asks       []Level
	ReceivedAt time.Time
}

// book package should maintain this and server package sends this to clients
type OrderBook struct {
	Symbol        Symbol
	Bids          []Level // sorted descending by price - best bid first
	Asks          []Level // sorted ascending by price  - best ask first
	LastUpdatedAt time.Time
}

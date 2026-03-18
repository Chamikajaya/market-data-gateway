package binance

import (
	"context"
	"fmt"
	"strings"

	"market-gw.com/internal/domain"
)

const (
	restBaseURL = "https://api.binance.com"
	wsBaseURL   = "wss://stream.binance.com:9443/ws"
	adapterName = "binance"
)

type Adapter struct {
	symbols []domain.Symbol
	depth   int
}

func NewAdapter(symbols []domain.Symbol, depth int) *Adapter {
	return &Adapter{
		symbols: symbols,
		depth:   depth,
	}
}

func (a *Adapter) Name() string {
	return adapterName
}

func (a *Adapter) Run(ctx context.Context) (<-chan domain.Update, error) {

	if len(a.symbols) == 0 {
		return nil, fmt.Errorf("binance: no symbols have been configured")
	}

	// TODO: Needs to run the Run function for each symbol configured in its own goroutine -> And then need to fan-in to a single merged channel
	for _, sym := range a.symbols {
		_, _ = a.runSymbol(ctx, sym)
	}

	return nil, nil

}

// TODO: Need to implement this according to -> "https://developers.binance.com/docs/derivatives/usds-margined-futures/websocket-market-streams/How-to-manage-a-local-order-book-correctly"
// * managing the full lifecycle for one symbol
// buffer ws events -> fetch REST snapshot -> synchronize -> stream incremental deltas
func (a *Adapter) runSymbol(ctx context.Context, sym domain.Symbol) (<-chan domain.Update, error) {
	// TODO:

	// ! TEMP USAGE TO PASS THE LINT
	_ = canonicalToBinanceNative(sym)

	var r restDepthResponse
	var w wsDepthEvent

	_ = binancePairsToLevels(r.Bids)
	_ = w.Symbol

	return nil, nil
}

// GET https://api.binance.com/api/v3/depth
type restDepthResponse struct {
	LastUpdateID int         `json:"lastUpdateId"`
	Bids         [][2]string `json:"bids"`
	Asks         [][2]string `json:"asks"`
}

// wss://stream.binance.com:9443/ws/btcusdt@depth
type wsDepthEvent struct {
	EventType     string      `json:"e"`
	EventTime     int64       `json:"E"`
	Symbol        string      `json:"s"`
	FirstUpdateID int64       `json:"U"`
	FinalUpdateID int64       `json:"u"`
	Bids          [][2]string `json:"b"`
	Asks          [][2]string `json:"a"`
}

// "BTC-USD" --> "BTCUSDT" || "ETH-USD" --> "ETHUSDT"
func canonicalToBinanceNative(sym domain.Symbol) string {
	s := strings.ReplaceAll(string(sym), "-", "")
	if strings.HasSuffix(s, "USD") {
		s = s[:len(s)-3] + "USDT"
	}
	return strings.ToUpper(s)
}

func binancePairsToLevels(pairs [][2]string) []domain.Level {

	if len(pairs) == 0 {
		return nil
	}

	levels := make([]domain.Level, len(pairs))
	for i, p := range pairs {
		levels[i] = domain.Level{Price: p[0], Quantity: p[1]}
	}
	return levels

}

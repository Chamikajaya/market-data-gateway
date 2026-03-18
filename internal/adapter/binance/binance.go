package binance

import (
	"context"
	"fmt"

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

	return nil, nil
}

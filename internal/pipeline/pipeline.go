// implements the fan in stage that merges per-exchange update channels into a single unified stream for the rest of the gateway

package pipeline

import (
	"context"
	"fmt"
	"log/slog"

	"market-gw.com/internal/book"
	"market-gw.com/internal/domain"
	"market-gw.com/internal/exchange"
)

type Pipeline struct {
	exchanges []exchange.Exchange
	registry  *book.Registry
}

func NewPipeline(exchanges []exchange.Exchange, registry *book.Registry) *Pipeline {
	return &Pipeline{
		exchanges: exchanges,
		registry:  registry,
	}
}

// ! TODO: Name() method ?

func (p *Pipeline) Run(ctx context.Context) (<-chan domain.Update, error) {

	if (len(p.exchanges)) == 0 {
		return nil, fmt.Errorf("pipeline: no exchange configured")
	}

	sources := make([]<-chan domain.Update, 0, len(p.exchanges))
	for _, exc := range p.exchanges {
		ch, err := exc.Run(ctx)
		if err != nil {
			return nil, fmt.Errorf("pipeline: start exchange %q: %w", exc.Name(), err) // %q -> print exchange name inside ""
		}
		slog.Info("pipeline: exchange started", "exchange", exc.Name())
		sources = append(sources, ch)
	}

	return nil, nil
}

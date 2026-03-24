// implements the fan in stage that merges per-exchange update channels into a single unified stream for the rest of the gateway

package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"market-gw.com/internal/book"
	"market-gw.com/internal/domain"
)

// pipeline is the consumer & currently binance is the implementation
type Exchanger interface {
	Run(ctx context.Context) (<-chan domain.Update, error)
	Name() string
}

type Pipeline struct {
	exchanges []Exchanger
	registry  *book.Registry
}

func NewPipeline(exchanges []Exchanger, registry *book.Registry) *Pipeline {
	return &Pipeline{
		exchanges: exchanges,
		registry:  registry,
	}
}

// returning the channel that emits the canonical Symbol for every book that has just been updated
func (p *Pipeline) Run(ctx context.Context) (<-chan domain.Symbol, error) {

	if (len(p.exchanges)) == 0 {
		return nil, fmt.Errorf("pipeline: no exchange configured")
	}

	sources := make([]<-chan domain.Update, 0, len(p.exchanges))
	for _, exc := range p.exchanges {
		ch, err := exc.Run(ctx) // starting all exchange adapters
		if err != nil {
			return nil, fmt.Errorf("pipeline: start exchange %q: %w", exc.Name(), err) // %q -> print exchange name inside ""
		}
		slog.Info("pipeline: exchange started", "exchange", exc.Name())
		sources = append(sources, ch)
	}

	// intermediary channel - since we have multiple exchanges running at the exact same time , firing updates asynchronously, if multiple exchanges tried to write directly to the "out" channel / tries to write to Registry simultaneously from a dozen different goroutines, race conditions could occur. so to mitigate this, "merged" channel acts as a funnel. because of this applyAndForward and run in just 1 goroutine
	// * 1 evt / 250 ms = 4 evt per sec per symbol => 64 / 4 = ~15 sec safety margin
	merged := make(chan domain.Update, len(sources)*64) // !TODO: 64 to be taken from the config

	// output channel sent to server
	// * 256 -> absorb the initital burst of snapshots on startup
	out := make(chan domain.Symbol, 256) // TODO: to be taken from the config

	var wg sync.WaitGroup
	wg.Add(len(sources))

	for _, src := range sources {
		go func() {
			defer wg.Done()
			p.feedFrom(ctx, src, merged)
		}()
	}

	go func() {
		wg.Wait()
		close(merged)
		slog.Info("pipeline: all exchange sources exhausted, merged channel was closed")
	}()

	go func() {
		defer close(out)
		p.applyAndNotify(merged, out) // ! todo: need to change
		slog.Info("pipeline: apply goroutine exited, out channel closed")
	}()

	return out, nil
}

// take an update from an exchange and transfer it into the "merged" funnel
func (p *Pipeline) feedFrom(ctx context.Context, src <-chan domain.Update, merged chan<- domain.Update) {
	for {
		select {
		case update, ok := <-src:
			if !ok {
				return
			}
			select {
			case merged <- update:
			case <-ctx.Done():
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (p *Pipeline) applyAndNotify(merged <-chan domain.Update, out chan<- domain.Symbol) {
	for update := range merged {
		b := p.registry.GetOrCreate(update.Symbol)
		b.Apply(update)

		slog.Info("pipeline: applied update",
			"type", updateTypeName(update.Type),
			"symbol", update.Symbol,
			"exchange", update.Exchange,
			"bids", len(update.Bids),
			"asks", len(update.Asks),
		)

		// notifying the server of which symbol changed
		select {
		case out <- update.Symbol:
		default:
			slog.Info("pipeline: out channel full, dropping update",
				"symbol", update.Symbol,
			)
		}
	}
}

func updateTypeName(t domain.UpdateType) string {
	switch t {
	case domain.UpdateTypeSnapshot:
		return "snapshot"
	case domain.UpdateTypeDelta:
		return "delta"
	default:
		return "unknown"
	}
}

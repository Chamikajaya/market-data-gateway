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

// pipeline is the consumer & should be implemented by every exchange adapter
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

// starts all exchange adapters, merges their updates, applies them to the book registry, and returns a channel of BookKeys that were just updated.
func (p *Pipeline) Run(ctx context.Context) (<-chan domain.BookKey, error) {

	if (len(p.exchanges)) == 0 {
		return nil, fmt.Errorf("pipeline: no exchange configured")
	}

	sources := make([]<-chan domain.Update, 0, len(p.exchanges)) // slice of channels, each channel is the output of an exchange adapter

	// * A) starting all exchanges
	for _, exc := range p.exchanges {
		ch, err := exc.Run(ctx) // starting all exchange adapters - each exchange starts producing updates
		if err != nil {
			return nil, fmt.Errorf("pipeline: start exchange %q: %w", exc.Name(), err) // %q -> print exchange name inside ""
		}
		slog.Info("pipeline: exchange started", "exchange", exc.Name())
		sources = append(sources, ch)
	}

	// * B) Fan-In --> Merging all exchange channels into a single merged channel
	// intermediary channel - since we have multiple exchanges running at the exact same time , firing updates asynchronously, if multiple exchanges tried to write directly to the "out" channel / tries to write to Registry simultaneously from a dozen different goroutines, race conditions could occur. so to mitigate this, "merged" channel acts as a funnel. because of this applyAndForward and run in just 1 goroutine
	//  1 evt / 250 ms = 4 evt per sec per symbol => 64 / 4 = ~15 sec safety margin
	merged := make(chan domain.Update, len(sources)*64)

	// output channel sent to server
	out := make(chan domain.BookKey, 32) // 32 is reasonable buffer -> ~ num of books we are tracking

	var wg sync.WaitGroup
	wg.Add(len(sources))

	for _, src := range sources {
		go func() {
			defer wg.Done()
			forward(ctx, src, merged) // read from each exchange channel and forward to the merged channel
		}()
	}

	go func() {
		wg.Wait()
		close(merged)
		slog.Info("pipeline: all exchange sources exhausted, merged channel was closed")
	}()

	// * C) apply updates and notify server - run in just single goroutine 💪 - made this run as a single goroutine coz applyAndNotify() mutates the registry and since we do not want 2 updates racing to modify the same book simultaneously.
	go func() {
		defer close(out)
		p.applyAndNotify(merged, out)
		slog.Info("pipeline: apply goroutine exited, out channel closed")
	}()

	return out, nil
}

func forward(ctx context.Context, src <-chan domain.Update, dst chan<- domain.Update) {
	for {
		select {
		case u, ok := <-src:
			if !ok {
				return
			}
			select {
			case dst <- u:
			case <-ctx.Done():
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// read from the merged channel and apply to the correct book in the registry, then notify server by sending the BookKey to the out channel
func (p *Pipeline) applyAndNotify(merged <-chan domain.Update, out chan<- domain.BookKey) {
	for u := range merged {
		b := p.registry.GetOrCreate(u.Exchange, u.Symbol)
		b.Apply(u)

		key := domain.MakeBookKey(u.Exchange, u.Symbol)

		slog.Info("pipeline: applied update",
			"type", typeName(u.Type),
			"key", key,
			"bids", len(u.Bids),
			"asks", len(u.Asks),
		)

		select {
		case out <- key:
		default:
			slog.Warn("pipeline: out channel full, dropping notification", "key", key)
		}
	}
}

func typeName(t domain.UpdateType) string {
	switch t {
	case domain.UpdateTypeSnapshot:
		return "snapshot"
	case domain.UpdateTypeDelta:
		return "delta"
	default:
		return "unknown"
	}
}

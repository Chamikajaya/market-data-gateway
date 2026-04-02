package kraken

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"market-gw.com/internal/domain"
)

const (
	wsURL       = "wss://ws.kraken.com/v2"
	adapterName = "kraken"
	bufferSize  = 200
)

// Adapter connects to Kraken WS v2 for configured symbols.
// Kraken's WS v2 book channel sends the snapshot automatically after subscribe,
// so no separate REST fetch is needed.
type Adapter struct {
	symbols []domain.Symbol // Kraken-native symbols, e.g. "BTC/USD"
	depth   int
}

func NewAdapter(symbols []domain.Symbol, depth int) *Adapter {
	return &Adapter{
		symbols: symbols,
		depth:   depth,
	}
}

func (a *Adapter) Name() string { return adapterName }

func (a *Adapter) Run(ctx context.Context) (<-chan domain.Update, error) {
	if len(a.symbols) == 0 {
		return nil, fmt.Errorf("kraken: no symbols configured")
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("kraken: dial ws: %w", err)
	}

	// Build the symbol list as plain strings for the subscription message.
	syms := make([]string, len(a.symbols))
	for i, s := range a.symbols {
		syms[i] = string(s)
	}

	sub := subscribeMsg{
		Method: "subscribe",
		Params: subscribeParams{
			Channel: "book",
			Symbol:  syms,
			Depth:   a.depth,
		},
	}

	if err := conn.WriteJSON(sub); err != nil {
		conn.Close()
		return nil, fmt.Errorf("kraken: send subscribe: %w", err)
	}
	slog.Info("kraken: subscribed", "symbols", syms, "depth", a.depth)

	out := make(chan domain.Update, bufferSize)

	go func() {
		defer func() {
			close(out)
			conn.Close()
		}()

		a.readLoop(ctx, conn, out)
	}()

	return out, nil
}

// readLoop reads WS messages and converts them to domain.Update.
// Kraken WS v2 sends:
//   - {"method":"subscribe","result":{...},"success":true,...}  → ack (skip)
//   - {"channel":"heartbeat"}                                    → skip
//   - {"channel":"book","type":"snapshot","data":[{...}]}        → snapshot
//   - {"channel":"book","type":"update","data":[{...}]}          → delta
func (a *Adapter) readLoop(ctx context.Context, conn *websocket.Conn, out chan<- domain.Update) {
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			select {
			case <-ctx.Done():
			default:
				slog.Error("kraken: ws read", "error", err)
			}
			return
		}

		// Quick peek at the message to determine its kind.
		var peek peekMsg
		if err := json.Unmarshal(raw, &peek); err != nil {
			slog.Error("kraken: unmarshal peek", "error", err)
			continue
		}

		// Skip non-book messages (heartbeat, subscribe acks, status, etc.)
		if peek.Channel != "book" {
			continue
		}

		var msg bookMsg
		if err := json.Unmarshal(raw, &msg); err != nil {
			slog.Error("kraken: unmarshal book msg", "error", err)
			continue
		}

		if len(msg.Data) == 0 {
			continue
		}

		entry := msg.Data[0]

		var uType domain.UpdateType
		switch peek.Type {
		case "snapshot":
			uType = domain.UpdateTypeSnapshot
		case "update":
			uType = domain.UpdateTypeDelta
		default:
			continue
		}

		update := domain.Update{
			Exchange:   adapterName,
			Symbol:     domain.Symbol(entry.Symbol),
			Type:       uType,
			Bids:       krakenLevels(entry.Bids),
			Asks:       krakenLevels(entry.Asks),
			ReceivedAt: time.Now(),
		}

		select {
		case out <- update:
		case <-ctx.Done():
			return
		default:
			slog.Warn("kraken: buffer full, dropping", "symbol", entry.Symbol)
		}
	}
}

// ── JSON shapes ─────────────────────────────────────────────────────────────

type subscribeMsg struct {
	Method string          `json:"method"`
	Params subscribeParams `json:"params"`
}

type subscribeParams struct {
	Channel string   `json:"channel"`
	Symbol  []string `json:"symbol"`
	Depth   int      `json:"depth,omitempty"`
}

// peekMsg is used to quickly check the channel and type of a WS message.
type peekMsg struct {
	Channel string `json:"channel"`
	Type    string `json:"type"`
}

type bookMsg struct {
	Channel string      `json:"channel"`
	Type    string      `json:"type"`
	Data    []bookEntry `json:"data"`
}

type bookEntry struct {
	Symbol string        `json:"symbol"`
	Bids   []krakenLevel `json:"bids"`
	Asks   []krakenLevel `json:"asks"`
}

// krakenLevel represents a price point from Kraken WS v2.
// Kraken sends price and qty as JSON numbers (floats).
type krakenLevel struct {
	Price float64 `json:"price"`
	Qty   float64 `json:"qty"`
}

// ── Converters ──────────────────────────────────────────────────────────────

func krakenLevels(levels []krakenLevel) []domain.Level {
	if len(levels) == 0 {
		return nil
	}
	out := make([]domain.Level, len(levels))
	for i, l := range levels {
		out[i] = domain.Level{
			Price:    strconv.FormatFloat(l.Price, 'f', -1, 64),
			Quantity: strconv.FormatFloat(l.Qty, 'f', -1, 64),
		}
	}
	return out
}

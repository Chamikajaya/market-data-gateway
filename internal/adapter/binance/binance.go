package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"

	"market-gw.com/internal/domain"
)

// ! TODO:- Need to take the constants from the config instead
const (
	restBaseURL  = "https://api.binance.com"
	wsBaseURL    = "wss://stream.binance.com:9443/ws"
	adapterName  = "binance"
	wsBufferSize = 200 // ! TODO:- Need to take the constants from the config instead

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

	_, _ = a.fetchSnapshot(ctx, "test")

	native := canonicalToBinanceNative(sym)
	wsURL := fmt.Sprintf("%s/%s@depth", wsBaseURL, strings.ToLower(native)) // binance's ws endpoint requres lowercase

	// opens tcp connection to binance and upgrades to ws protocol
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("dial ws for %s: %w", sym, err)
	}

	buffer := make(chan wsDepthEvent, wsBufferSize)

	// ws reader goroutine
	go func() {
		defer func() {
			close(buffer)
			if err := conn.Close(); err != nil {
				slog.Error("failed to close connection", "error", err)
			}
		}()

		// read loop
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				select {
				case <-ctx.Done():
					// clean shutdown - not an error
				default:
					slog.Error("read ws", "err", err, "symbol", sym)
				}
				return // either way return and trigger the defer functions
			}

			var event wsDepthEvent
			if err := json.Unmarshal(msg, &event); err != nil {
				slog.Error("unmarshal ws event", "err", err, "symbol", sym)
				continue
			}

			select {
			case buffer <- event: // send the event to the buffer channel
			case <-ctx.Done():
				return
			default:
				slog.Warn("ws buffer full, dropping event", "symbol", sym)
			}

		}
	}()

	out := make(chan domain.Update, wsBufferSize)

	// go routine to run the snapshot -> sync -> stream
	// TODO:
	go func() {
		defer close(out)
	}()

	return out, nil

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

func (a *Adapter) fetchSnapshot(ctx context.Context, native string) (snap restDepthResponse, err error) {

	url := fmt.Sprintf("%s/api/v3/depth?symbol=%s&limit=%d", restBaseURL, native, a.depth)

	// use WithContext for graceful shutdown -> so that if ctrl+c comes while this request is made, go will kill the request instead of leaving the go routine hanging
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return restDepthResponse{}, fmt.Errorf("build request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return restDepthResponse{}, fmt.Errorf("http get: %w", err)
	}
	// closing the TCP connection
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close body: %w", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		// ! TODO: Get 512 from config file instead
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512)) // reading the first 512 bytes only in error response
		return restDepthResponse{}, fmt.Errorf("http %d: %s", resp.StatusCode, body)

	}

	var result restDepthResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return restDepthResponse{}, fmt.Errorf("decode response: %w", err)
	}

	return result, nil

}

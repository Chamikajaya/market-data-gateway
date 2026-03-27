### 2026-03-20 — Binance REST/WS synchronization adapter

**Goal**: Implement binance adapter.go to fetch REST snapshots and sync with live WebSocket deltas.

**What worked**:

- Mapping Binance JSON shapes (restDepthResponse, wsDepthEvent) to the canonical domain.Update went smoothly

- Fan-in pattern using sync.WaitGroup successfully merged multiple per-symbol channels into one unified stream without mutexes

**What broke (and why):**

- High memory risk on bad HTTP responses >> io.ReadAll could load a massive error page and crash the app >> wrapped resp.Body in io.LimitReader to cap it at 512 bytes

- Race condition between REST and WS >> fetching REST first misses live trades >> opened WS to buffer events before firing the REST call to prevent sequence gaps

**Concept unlocked:**

- Structural cancellation >> passing ctx to http.NewRequestWithContext and websocket.DialContext ensures blocking IO instantly drops on shutdown instead of leaking goroutines

**Still fuzzy:**

- Whether the fanIn function truly belongs inside the Binance package or if it should be extracted to a shared pipeline utility when adding more exchanges

**Next**:

- Test the functionality end-to-end with binance adapter before moving on to the next exchange (kraken)

### 2026-03-23 — Core Pipeline and State Registry

**Goal**: Implement pipeline.go to merge multiple exchange streams into a single funnel and update the central order book Registry safely.

**What worked**:

- Using a consumer-defined Exchanger interface inside the pipeline package kept the core completely decoupled from exchange-specific logic.

What broke (and why):

- Slow downstream clients could freeze the entire gateway >> a standard channel send (out <- update) blocks if the buffer is full >> used a select with a default case to intentionally drop updates to protect the pipeline's speed.

**Concept unlocked:**

- Graceful degradation: in high-frequency trading systems, it is better to intentionally drop data on the floor (using non-blocking channel sends) than to let a slow consumer stall the central state machine.

**Still fuzzy:**

- How the actual WebSocket server is going to take this single out channel and broadcast it to hundreds of clients without creating a massive bottleneck.

**Next**:

- Implement server.go to handle downstream WebSocket clients.

### 2026-03-25 — WebSocket Server

**Goal:** Implement server.go to handle downstream WebSocket clients, manage symbol subscriptions, and safely broadcast updates without bottlenecking the gateway.

**What worked:**

- The current pattern kept the clients map completely lock-free. Since only the Hub goroutine touches the map, no mutexes were needed.
- Using an empty struct (map[domain.Symbol]struct{}) created a zero-byte memory footprint Set for tracking client subscriptions.

**What broke (and why):**

- Fatal panic from concurrent WebSocket writes >> gorilla/websocket explicitly forbids multiple goroutines writing to the same connection simultaneously >> fixed by strictly forcing sendInitialSnapshots to finish completely before launching the client's ongoing write goroutine.

**Concept unlocked:** "Share memory by communicating" >> instead of wrapping a global client map in a lock, giving the Hub exclusive ownership of the map and forcing clients to send a registration ticket over a channel entirely avoids race conditions.

**Still fuzzy:** In code review we had today, asked to consider creating ws connedctions at the start of the app and also advised not to create separate ws connection per each tracked symbol.

**Next:** Wire everything together in cmd/gateway/main.go using the os/signal package for graceful shutdown.

## 2026-03-26 — Wired up the Binance adapter end-to-end 💪

**Goal**: Get the gateway actually running with Binance, not just stubbed out.

**What worked**:

- **`main.go`** — was just a `fmt.Println("Hello")`. Wired it up with `signal.NotifyContext` for graceful shutdown (SIGINT/SIGTERM), created the `book.Registry`, `binance.Adapter`, `pipeline.Pipeline`, and `server.Server`, and connected them all together.

- **`domain/types.go`** — introduced `BookKey` type (`exchange:symbol` string) and a `MakeBookKey` helper. The idea is that the registry maps are keyed by `BookKey` not just `Symbol`, so we can have separate order books per exchange. Added `Exchange` field to `OrderBook` so downstream clients know where the data came from. Also added JSON struct tags to `Level` (`price`, `qty`).

- **`book/registry.go`** — changed the internal map from `map[domain.Symbol]*Book` → `map[domain.BookKey]*Book`. `GetOrCreate` now takes `(exchange, symbol)` and builds the key internally. `Symbols()` renamed to `Keys()` returning `[]domain.BookKey`.

- **`pipeline/pipeline.go`** — the `out` channel changed from `chan domain.Symbol` → `chan domain.BookKey`. Builds the key using `domain.MakeBookKey(u.Exchange, u.Symbol)` so the server knows both the exchange and symbol that was updated. Renamed helper functions for clarity (`feedFrom` → `forward`, `applyAndForward` → `applyAndNotify`).

- **`server/server.go`** — updated the client subscription protocol. Clients now send:

  ```json
  { "subscribe": [{ "exchange": "binance", "symbol": "BTCUSDT" }] }
  ```

  instead of the old `{"subscribe": ["BTC-USD"]}`. The hub, registrations, and notifications all use `domain.BookKey`. The JSON messages sent to clients now include an `"exchange"` field.

- **`adapter/binance/binance.go`** — removed the `canonicalToBinanceNative` function. Previously it converted `"BTC-USD"` → `"BTCUSDT"`, but now clients just pass the native Binance symbol directly.

- **`config/config.go`** — was empty. Added a `Config` struct and `Load()` that parses CLI flags: `--symbols` (comma-separated), `--depth`, `--addr`.

**Concepts Unlocked**:

- `signal.NotifyContext` — cleaner than manually handling `os.Signal` channels. Returns a context that gets canceled on SIGINT/SIGTERM.
- `RLock` vs `RUnlock` — must pair correctly or the mutex deadlocks. `RLock()` → `RUnlock()`, `Lock()` → `Unlock()`.
- Composite map keys — using a typed string (`BookKey = "binance:BTCUSDT"`) as a map key is simple and avoids needing a struct key (structs work but are more verbose).

**Next**: Implement the end to end flow for Kraken

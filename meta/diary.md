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

## 03/27 & 03/30— Integrated Kraken exchange adapter

**Goal**: Add Kraken as a second exchange so clients can get order books from both Binance and Kraken.

**What Worked**:

- **`adapter/kraken/kraken.go`** — built the full Kraken adapter. Key differences from Binance:
  - **No REST sync needed** — Kraken's WS v2 `book` channel sends the snapshot automatically after we subscribe, so there is no need for the REST-fetch-then-sync process that Binance requires.
  - **Single WS connection** — all symbols go through one connection. After dialing `wss://ws.kraken.com/v2`, we send one subscribe message listing all symbols and receive snapshots + updates for all of them on that connection.
  - **Subscribe message format**:
    ```json
    {
      "method": "subscribe",
      "params": {
        "channel": "book",
        "symbol": ["BTC/USD", "ETH/USD"],
        "depth": 10
      }
    }
    ```
  - **Message parsing** — Kraken WS v2 sends several message types on the same connection: heartbeats, subscribe acks, and book data. Used a `peekMsg` struct with just `channel` and `type` to quickly classify messages before full parsing. Only messages with `channel == "book"` get parsed as `bookMsg`.
  - **Price/quantity format** — Kraken sends `price` and `qty` as JSON floats (e.g., `0.5666`, `4831.75496356`), unlike Binance which sends string pairs (`["87255.99","0.01"]`). Used `strconv.FormatFloat(price, 'f', -1, 64)` to convert to string without losing precision. The `-1` precision flag tells Go to use the minimum number of digits needed to represent the float exactly.
  - **Symbol format** — Kraken uses slash-separated pairs like `BTC/USD`, `ETH/USD` (not `BTCUSDT`). We pass these through as-is.

**Concepts unlocked**:

- **Duck typing / interface satisfaction** — both `binance.Adapter` and `kraken.Adapter` implement `pipeline.Exchanger` (the `Run(ctx) (<-chan domain.Update, error)` + `Name() string` interface) without explicitly declaring it. Go checks at compile time.

- **`strconv.FormatFloat` with precision `-1`** — this tells Go to use the shortest representation that, when parsed back, produces the exact same float64. This avoids ugly trailing zeros or imprecise decimal expansions.
- **Message classification pattern** — parsing a "peek" struct first (`{channel, type}`) to route messages before doing the full unmarshal is efficient and avoids parsing heartbeats/acks as book data.

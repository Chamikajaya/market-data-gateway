### 2026-03-20 — Binance REST/WS synchronization adapter
**Goal**: Implement binance adapter.go to fetch REST snapshots and sync with live WebSocket deltas.

**What worked**:
* Mapping Binance JSON shapes (restDepthResponse, wsDepthEvent) to the canonical domain.Update went smoothly

* Fan-in pattern using sync.WaitGroup successfully merged multiple per-symbol channels into one unified stream without mutexes

**What broke (and why):**



* High memory risk on bad HTTP responses >> io.ReadAll could load a massive error page and crash the app >> wrapped resp.Body in io.LimitReader to cap it at 512 bytes

* Race condition between REST and WS >> fetching REST first misses live trades >> opened WS to buffer events before firing the REST call to prevent sequence gaps

**Concept unlocked:**
* Structural cancellation >> passing ctx to http.NewRequestWithContext and websocket.DialContext ensures blocking IO instantly drops on shutdown instead of leaking goroutines

**Still fuzzy:**

* Whether the fanIn function truly belongs inside the Binance package or if it should be extracted to a shared pipeline utility when adding more exchanges

**Next**:

* Test the functionality end-to-end with binance adapter before moving on to the next exchange (kraken)


### 2026-03-23 — Core Pipeline and State Registry
**Goal**: Implement pipeline.go to merge multiple exchange streams into a single funnel and update the central order book Registry safely.

**What worked**:

* Using a consumer-defined Exchanger interface inside the pipeline package kept the core completely decoupled from exchange-specific logic.


What broke (and why):

* Slow downstream clients could freeze the entire gateway >> a standard channel send (out <- update) blocks if the buffer is full >> used a select with a default case to intentionally drop updates to protect the pipeline's speed.

**Concept unlocked:**

* Graceful degradation: in high-frequency trading systems, it is better to intentionally drop data on the floor (using non-blocking channel sends) than to let a slow consumer stall the central state machine.

**Still fuzzy:**

* How the actual WebSocket server is going to take this single out channel and broadcast it to hundreds of clients without creating a massive bottleneck.

**Next**:

* Implement server.go to handle downstream WebSocket clients.
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

### 2026-03-25 — WebSocket Server 

**Goal:** Implement server.go to handle downstream WebSocket clients, manage symbol subscriptions, and safely broadcast updates without bottlenecking the gateway.

**What worked:** 
* The current pattern kept the clients map completely lock-free. Since only the Hub goroutine touches the map, no mutexes were needed.
* Using an empty struct (map[domain.Symbol]struct{}) created a zero-byte memory footprint Set for tracking client subscriptions.

**What broke (and why):** 
* Fatal panic from concurrent WebSocket writes >> gorilla/websocket explicitly forbids multiple goroutines writing to the same connection simultaneously >> fixed by strictly forcing sendInitialSnapshots to finish completely before launching the client's ongoing write goroutine.

**Concept unlocked:** "Share memory by communicating" >> instead of wrapping a global client map in a lock, giving the Hub exclusive ownership of the map and forcing clients to send a registration ticket over a channel entirely avoids race conditions.

**Still fuzzy:** In code review we had today, asked to consider creating ws connedctions at the start of the app and also advised not to create separate ws connection per each tracked symbol.

**Next:** Wire everything together in cmd/gateway/main.go using the os/signal package for graceful shutdown.
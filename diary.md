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
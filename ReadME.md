# Market Data Gateway

A multi-exchange cryptocurrency order book gateway written in Go. Connects to **Binance** and **Kraken**, maintains internal order book state, and exposes a single WebSocket server for downstream clients.

## Quick Start

```bash
# Clone and enter the project
git clone  https://github.com/Chamikajaya/market-data-gateway
cd market-data-gateway

# Run with defaults (Binance BTCUSDT + Kraken BTC/USD on port 8080)
go run ./cmd/gateway/
```

## CLI Flags

| Flag        | Description                     | Default   |
| ----------- | ------------------------------- | --------- |
| `--binance` | Comma-separated Binance symbols | `BTCUSDT` |
| `--kraken`  | Comma-separated Kraken symbols  | `BTC/USD` |
| `--depth`   | Order book depth for snapshots  | `10`      |
| `--addr`    | WebSocket server listen address | `:8080`   |

## Example Commands

```bash
# Single symbol, single exchange
go run ./cmd/gateway/ --binance BTCUSDT --kraken ""

# Multiple symbols, both exchanges
go run ./cmd/gateway/ --binance BTCUSDT,ETHUSDT,SOLUSDT --kraken BTC/USD,ETH/USD,SOL/USD

# Custom port and depth
go run ./cmd/gateway/ --binance BTCUSDT,ETHUSDT --kraken BTC/USD,ETH/USD --depth 20 --addr :9090

```

## Connecting as a Client

Connect via WebSocket to `ws://localhost:8080/ws` and send a subscription message **within 10 seconds**:

```json
{ "subscribe": [{ "exchange": "binance", "symbol": "BTCUSDT" }] }
```

### Subscription Examples

**Single coin, single exchange:**

```json
{ "subscribe": [{ "exchange": "binance", "symbol": "BTCUSDT" }] }
```

**Multiple coins, same exchange:**

```json
{
  "subscribe": [
    { "exchange": "kraken", "symbol": "BTC/USD" },
    { "exchange": "kraken", "symbol": "ETH/USD" }
  ]
}
```

**Cross-exchange:**

```json
{
  "subscribe": [
    { "exchange": "binance", "symbol": "BTCUSDT" },
    { "exchange": "kraken", "symbol": "BTC/USD" },
    { "exchange": "binance", "symbol": "ETHUSDT" },
    { "exchange": "kraken", "symbol": "ETH/USD" }
  ]
}
```

**No subscription message** — if you don't send anything within 10 seconds, you receive updates for **all** configured symbols from **all** exchanges.

### Response Format

**Snapshot** (sent once on connect per subscribed book):

```json
{
  "type": "snapshot",
  "exchange": "binance",
  "symbol": "BTCUSDT",
  "bids": [{ "price": "87255.99000000", "qty": "0.01000000" }],
  "asks": [{ "price": "87256.00000000", "qty": "0.50000000" }],
  "timestamp": "2026-03-26T09:31:17.123456789+05:30"
}
```

**Delta** (continuous updates after the snapshot):

```json
{
  "type": "delta",
  "exchange": "binance",
  "symbol": "BTCUSDT",
  "bids": [{ "price": "87255.50000000", "qty": "0.00000000" }],
  "asks": [{ "price": "87257.00000000", "qty": "1.20000000" }],
  "timestamp": "2026-03-26T09:31:18.456789012+05:30"
}
```

> A bid/ask with `qty: "0"` means that price level was removed from the book.

## Testing with Postman

1. Open Postman → **New** → **WebSocket**
2. Enter `ws://localhost:8080/ws` and click **Connect**
3. Immediately paste and send a subscription message (within 1 second):
   ```json
   {
     "subscribe": [
       { "exchange": "binance", "symbol": "BTCUSDT" },
       { "exchange": "kraken", "symbol": "ETH/USD" }
     ]
   }
   ```
4. You'll see a snapshot followed by a stream of deltas in the Messages panel

## Symbol Reference

| Exchange | Symbol Format        | Examples                                   |
| -------- | -------------------- | ------------------------------------------ |
| Binance  | Concatenated pair    | `BTCUSDT`, `ETHUSDT`, `SOLUSDT`, `BNBUSDT` |
| Kraken   | Slash-separated pair | `BTC/USD`, `ETH/USD`, `SOL/USD`, `XRP/USD` |

## Graceful Shutdown

Press `Ctrl+C` to stop. The gateway will:

1. Stop accepting new client connections
2. Close all existing client WebSocket connections
3. Disconnect from exchange WebSockets
4. Wait for all goroutines to exit
5. Print `market-data-gateway stopped`

package config

import (
	"flag"
	"strings"
	"time"

	"market-gw.com/internal/domain"
)

type Config struct {
	BinanceSymbols []domain.Symbol
	KrakenSymbols  []domain.Symbol
	Depth          int
	Addr           string
	WriteTimeout   time.Duration
	PingInterval   time.Duration
}

func Load() Config {
	var (
		binanceSymbols string
		krakenSymbols  string
		depth          int
		addr           string
	)

	flag.StringVar(&binanceSymbols, "binance", "BTCUSDT", "comma-separated Binance symbols (e.g. BTCUSDT,ETHUSDT)")
	flag.StringVar(&krakenSymbols, "kraken", "BTC/USD", "comma-separated Kraken symbols (e.g. BTC/USD,ETH/USD)")
	flag.IntVar(&depth, "depth", 10, "order book depth")
	flag.StringVar(&addr, "addr", ":8080", "WebSocket server listen address")
	flag.Parse()

	return Config{
		BinanceSymbols: parseSymbols(binanceSymbols),
		KrakenSymbols:  parseSymbols(krakenSymbols),
		Depth:          depth,
		Addr:           addr,
		WriteTimeout:   5 * time.Second,
		PingInterval:   30 * time.Second,
	}
}

func parseSymbols(raw string) []domain.Symbol {
	var syms []domain.Symbol
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			syms = append(syms, domain.Symbol(s))
		}
	}
	return syms
}

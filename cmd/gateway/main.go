package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"market-gw.com/internal/adapter/binance"
	"market-gw.com/internal/adapter/kraken"
	"market-gw.com/internal/book"
	"market-gw.com/internal/config"
	"market-gw.com/internal/pipeline"
	"market-gw.com/internal/server"
)

func main() {
	cfg := config.Load()

	slog.Info("starting market-data-gateway",
		"binance_symbols", cfg.BinanceSymbols,
		"kraken_symbols", cfg.KrakenSymbols,
		"depth", cfg.Depth,
		"addr", cfg.Addr,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	registry := book.NewRegistry()

	var exchanges []pipeline.Exchanger

	if len(cfg.BinanceSymbols) > 0 {
		exchanges = append(exchanges, binance.NewAdapter(cfg.BinanceSymbols, cfg.Depth))
	}
	if len(cfg.KrakenSymbols) > 0 {
		exchanges = append(exchanges, kraken.NewAdapter(cfg.KrakenSymbols, cfg.Depth))
	}

	p := pipeline.NewPipeline(exchanges, registry)

	changed, err := p.Run(ctx)
	if err != nil {
		slog.Error("pipeline failed to start", "error", err)
		os.Exit(1)
	}

	srv := server.NewServer(server.Config{
		Addr:         cfg.Addr,
		WriteTimeout: cfg.WriteTimeout,
		PingInterval: cfg.PingInterval,
	}, registry, changed)

	if err := srv.Run(ctx); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}

	slog.Info("market-data-gateway stopped")
}

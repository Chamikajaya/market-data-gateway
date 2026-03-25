// runs the downstream ws server - accepts client connections - send book snapshots on initial connect and then streams incremental delta updates as they arrive from the pipeline

package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"market-gw.com/internal/book"
	"market-gw.com/internal/domain"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // allowing every origin
}

type bookMessage struct {
	Type      string         `json:"type"`
	Symbol    string         `json:"symbol"`
	Bids      []domain.Level `json:"bids"`
	Asks      []domain.Level `json:"asks"`
	Timestamp string         `json:"timestamp"`
}

type subscribeMessage struct {
	Subscribe []string `json:"subscribe"`
}

// what the hub sends to a client goroutine - when the pipeline tells the hub that a symbol has changed, the hub sends this to the client's personal channel
type notification struct {
	Symbol domain.Symbol
	isFull bool // true if the update is a full snapshot, false if it's a delta update
}

// sent by a new client to the hub
type registration struct {
	symbols  map[domain.Symbol]struct{} // symbols the client is interested in - created a set here since we need to check whether a symbol exists in the client's subscription list or not
	notifyCh chan<- notification        // hub is allowed to write only
	done     <-chan struct{}            // to signal the hub that the client has disconnected - hub is able to read only
}

type Config struct {
	Addr         string
	WriteTimeout time.Duration // how long to wait before timing out a write to a client
	PingInterval time.Duration // how often to send ping messages to clients to check if they are still alive
}

type Server struct {
	cfg      Config
	registry *book.Registry

	register chan registration
	changed  <-chan domain.Symbol // from pipeline - which symbol changed
}

func NewServer(cfg Config, registry *book.Registry, changed <-chan domain.Symbol) *Server {
	return &Server{
		cfg:      cfg,
		registry: registry,
		register: make(chan registration, 32), // registration queue limit = 32 --> if more than 32 clients try to connect at the same time, the 33rd client will be blocked until there is space - to now overwhelm the hub
		changed:  changed,
	}
}

func (s *Server) Run(ctx context.Context) error {

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)

	srv := &http.Server{
		Addr:    s.cfg.Addr,
		Handler: mux,
	}

	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return err
	}
	slog.Info("server: listening for client connections", "addr", s.cfg.Addr)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.runHub(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("server: HTTP server error", "error", err)
		}
	}()

	<-ctx.Done()
	slog.Info("server: shutdown signal received, shutting down HTTP server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // ! take from config
	defer cancel()
	srv.Shutdown(shutdownCtx)

	wg.Wait()
	slog.Info("server: shutdown complete")
	return nil

}

// should be responsible for client map - receive symbol change notifications from pipeline and then fan out to relevant clients
func (s *Server) runHub(ctx context.Context) {

	// client map
	clients := make(map[chan<- notification]registration)

	for {
		select {
		case reg := <-s.register:
			clients[reg.notifyCh] = registration{
				symbols:  reg.symbols,
				notifyCh: reg.notifyCh,
				done:     reg.done,
			}
			slog.Info("server: new client registered", "symbols", reg.symbols, "total_clients", len(clients))

		case sym, ok := <-s.changed: // listening to the pipeline
			if !ok {
				// pipeline has closed the channel - no more updates will come - so closing all client  channels
				slog.Info("server: pipeline closed, disconnecting", "remaining_clients", len(clients))
				for _, entry := range clients {
					close(entry.notifyCh)
				}
				return
			}
			for ch, entry := range clients {
				// before hub sends update to a client checking their entry.done channel
				select {
				case <-entry.done: // if the client is disconnected, delete the client from the client map
					delete(clients, ch)
					slog.Info("server: client disconnected", "symbols", entry.symbols, "total_clients", len(clients))
					continue
				default:
				}

				// filter by subscription
				if entry.symbols != nil {
					// if the curr update is not for the asked symbol by the client - skip and continue
					if _, want := entry.symbols[sym]; !want {
						continue
					}
				}

				// if the client is still connected and wants updates for this symbol, send the notification - if the client's notify channel buffer is full, skip sending the notification to avoid blocking the hub
				select {
				case ch <- notification{Symbol: sym}:
				default:
					slog.Info("server: notify buffer full for client", "symbol", sym)
				}
			}

		case <-ctx.Done():
			for _, entry := range clients {
				close(entry.notifyCh)
			}
			return
		}
	}
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("server: failed to upgrade WebSocket connection", "error", err)
		return
	}

	defer conn.Close()

	// reading optional subscription message from client - for now giving 1 second to read the message - if no message received, sending all snapshots for all symbols
	var symbols map[domain.Symbol]struct{}
	conn.SetReadDeadline(time.Now().Add(1 * time.Second)) // ! todo: take from config
	_, raw, err := conn.ReadMessage()
	if err == nil {
		var sub subscribeMessage
		if json.Unmarshal(raw, &sub) == nil && len(sub.Subscribe) > 0 {
			symbols = make(map[domain.Symbol]struct{}, len(sub.Subscribe))
			for _, s := range sub.Subscribe {
				symbols[domain.Symbol(s)] = struct{}{}
			}
			slog.Info("server: client subscription", "symbols", sub.Subscribe)
		}
	}

	// clearing read deadline
	conn.SetReadDeadline(time.Time{})

	notifyCh := make(chan notification, 64) // !TODO: 64
	done := make(chan struct{})

	s.register <- registration{
		symbols:  symbols,
		notifyCh: notifyCh,
		done:     done,
	}

	// send initial snapshots for the subscribed symbols
	s.sendInitialSnapshots(conn, symbols)

	// read goroutine - pong handling and disconnection detection
	go func() {
		defer close(done)
		conn.SetReadDeadline(time.Now().Add(s.cfg.PingInterval * 2)) // if no message received in 2 ping intervals, consider the client disconnected
		conn.SetPongHandler(func(string) error {
			conn.SetReadDeadline(time.Now().Add(s.cfg.PingInterval * 2))
			return nil
		})

		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}

	}()

	// track which symbols this client has already received snapshots for, first notification for a symbol should trigger a full snapshot send, subsequent notifications for the same symbol can be sent as deltas
	seenSnapshot := make(map[domain.Symbol]bool)
	for _, sym := range s.registry.Symbols() {
		if symbols == nil {
			seenSnapshot[sym] = true
		} else if _, ok := symbols[sym]; ok {
			seenSnapshot[sym] = true
		}

	}
	ticker := time.NewTicker(s.cfg.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case notify, ok := <-notifyCh:
			if !ok {
				return // shut down the hub
			}
			book, exists := s.registry.Get(notify.Symbol)
			if !exists {
				continue
			}
			snap := book.BookSnapshot()
			msgType := "delta"
			if !seenSnapshot[notify.Symbol] {
				msgType = "snapshot"
				seenSnapshot[notify.Symbol] = true
			}
			msg := bookMessage{
				Type:      msgType,
				Symbol:    string(snap.Symbol),
				Bids:      snap.Bids,
				Asks:      snap.Asks,
				Timestamp: snap.LastUpdatedAt.Format(time.RFC3339Nano),
			}
			if err := writeMsg(conn, msg, s.cfg.WriteTimeout); err != nil {
				slog.Info("server: failed to send message to client",
					"symbol", snap.Symbol,
					"error", err,
				)
				return
			}

		case <-ticker.C:
			conn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-done:
			return
		}
	}
}

// to send the current full book for the requested symbols to a newly connected client
func (s *Server) sendInitialSnapshots(conn *websocket.Conn, symbols map[domain.Symbol]struct{}) {
	all := s.registry.Symbols()
	for _, sym := range all {
		if symbols != nil {
			if _, ok := symbols[sym]; !ok {
				continue
			}
		}
		book, ok := s.registry.Get(sym)
		if !ok {
			continue
		}
		snap := book.BookSnapshot()
		if snap.LastUpdatedAt.IsZero() {
			continue
		}
		msg := bookMessage{
			Type:      "snapshot",
			Symbol:    string(snap.Symbol),
			Bids:      snap.Bids,
			Asks:      snap.Asks,
			Timestamp: snap.LastUpdatedAt.Format(time.RFC3339Nano),
		}
		if err := writeMsg(conn, msg, s.cfg.WriteTimeout); err != nil {
			slog.Info("server: failed to send initial snapshot",
				"symbol", snap.Symbol,
				"error", err,
			)
			return
		}
	}
}

func writeMsg(conn *websocket.Conn, msg bookMessage, timeout time.Duration) error {
	conn.SetWriteDeadline(time.Now().Add(timeout))
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

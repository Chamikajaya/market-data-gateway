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
	CheckOrigin: func(r *http.Request) bool { return true },
}

// sent to clients.
type bookMessage struct {
	Type      string         `json:"type"`
	Exchange  string         `json:"exchange"`
	Symbol    string         `json:"symbol"`
	Bids      []domain.Level `json:"bids"`
	Asks      []domain.Level `json:"asks"`
	Timestamp string         `json:"timestamp"`
}

// clients send on connect -> Eg: {"subscribe": [{"exchange":"binance","symbol":"BTCUSDT"}]}
type subscribeRequest struct {
	Subscribe []struct {
		Exchange string `json:"exchange"`
		Symbol   string `json:"symbol"`
	} `json:"subscribe"`
}

// what the hub sends to a per-client channel when a book changes
type notification struct {
	key domain.BookKey
}

type registration struct {
	keys     map[domain.BookKey]struct{} // what the client subscribed to
	notifyCh chan<- notification         // hub writes here
	done     <-chan struct{}             // hub reads to detect disconnect
}

type Config struct {
	Addr         string
	WriteTimeout time.Duration
	PingInterval time.Duration
}

type Server struct {
	cfg      Config
	registry *book.Registry

	register chan registration     // clients send registration requests here
	changed  <-chan domain.BookKey // pipeline sends changed book keys here
}

func NewServer(cfg Config, registry *book.Registry, changed <-chan domain.BookKey) *Server {
	return &Server{
		cfg:      cfg,
		registry: registry,
		register: make(chan registration, 32),
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
	slog.Info("server: listening", "addr", s.cfg.Addr)

	var wg sync.WaitGroup

	wg.Add(1)
	// launching the hub goroutine
	go func() {
		defer wg.Done()
		s.runHub(ctx)
	}()

	wg.Add(1)
	// launching the http server goroutine
	go func() {
		defer wg.Done()
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("server: HTTP error", "error", err)
		}
	}()

	<-ctx.Done()
	slog.Info("server: shutting down")
	
	// graceful shutdown with 5 seconds timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
	
	// waits for the hub and http server goroutines to exit
	wg.Wait()
	slog.Info("server: shutdown complete")
	return nil
}

func (s *Server) runHub(ctx context.Context) {

	// runHub is run in a single goroutine + owns the client map -> no mutexes needed since there is no concurrent access to the client map
	clients := make(map[chan<- notification]registration)  // key: client's notification channel - each channel is unique in go

	for {
		select {
		// CASE 1 :- New client registration
		case reg := <-s.register:
			clients[reg.notifyCh] = reg
			slog.Info("server: client registered", "keys", reg.keys, "total", len(clients))
		
		// CASE 2 :- A book changed, notify interested clients
		case key, ok := <-s.changed:
			// if pipeline is done, closing every client's notification channel
			if !ok {
				slog.Info("server: pipeline closed, disconnecting clients", "count", len(clients))
				for _, entry := range clients {
					close(entry.notifyCh)
				}
				return
			}
			
			// fan out to interested clients
			for ch, entry := range clients {
				select {
				case <-entry.done:
					delete(clients, ch)
					slog.Info("server: client gone", "total", len(clients))
					continue
				default:
				}

				// filter by subscription keys - if they sent a subscription message with specific keys, only notify if the changed key is in their subscription list. If they did not send a subscription message, notify for every change.
				if entry.keys != nil {
					if _, want := entry.keys[key]; !want {
						continue
					}
				}

				// Send the notification
				select {
				case ch <- notification{key: key}:
				default:
					slog.Warn("server: notify buffer full", "key", key)
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
		slog.Error("server: upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	// ! Read subscription message from the client (10 second deadline)
	var keys map[domain.BookKey]struct{}
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err == nil {
		var sub subscribeRequest
		if json.Unmarshal(raw, &sub) == nil && len(sub.Subscribe) > 0 {
			keys = make(map[domain.BookKey]struct{}, len(sub.Subscribe))
			for _, s := range sub.Subscribe {
				k := domain.MakeBookKey(s.Exchange, domain.Symbol(s.Symbol))
				keys[k] = struct{}{}
			}
			slog.Info("server: client subscription", "keys", keys)
		}
	}
	conn.SetReadDeadline(time.Time{})

	notifyCh := make(chan notification, 64)  // hub writes notifications here, buffered to avoid blocking the hub
	done := make(chan struct{})  // closed when the client disconnects

	// register this client with the hub
	s.register <- registration{
		keys:     keys,
		notifyCh: notifyCh,
		done:     done,
	}

	// send initial snapshots for subscribed books.
	s.sendInitialSnapshots(conn, keys)

	// read goroutine for pong handling and disconnect detection.
	go func() {
		defer close(done)
		conn.SetReadDeadline(time.Now().Add(s.cfg.PingInterval * 2))  // send a ping every PingInterval
		conn.SetPongHandler(func(string) error {
			conn.SetReadDeadline(time.Now().Add(s.cfg.PingInterval * 2))  // PongHandler received, connection is alive, extend the read deadline
			return nil
		})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	
	// track which books already got initial snapshots - so that when the hub sends teh first notification for a book, send it as "delta" not "snapshot"
	seenSnapshot := make(map[domain.BookKey]bool)
	for _, k := range s.registry.Keys() {
		if keys == nil {
			seenSnapshot[k] = true
		} else if _, ok := keys[k]; ok {
			seenSnapshot[k] = true
		}
	}

	ticker := time.NewTicker(s.cfg.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case n, ok := <-notifyCh:
			if !ok {
				return
			}
			bk, exists := s.registry.Get(n.key)
			if !exists {
				continue
			}
			snap := bk.Snapshot()
			msgType := "delta"
			if !seenSnapshot[n.key] {
				msgType = "snapshot"
				seenSnapshot[n.key] = true
			}
			msg := bookMessage{
				Type:      msgType,
				Exchange:  snap.Exchange,
				Symbol:    string(snap.Symbol),
				Bids:      snap.Bids,
				Asks:      snap.Asks,
				Timestamp: snap.LastUpdatedAt.Format(time.RFC3339Nano),
			}
			if err := writeJSON(conn, msg, s.cfg.WriteTimeout); err != nil {
				slog.Info("server: write failed", "key", n.key, "error", err)
				return
			}

		case <-ticker.C:  // ws ping to check if the client is still alive
			conn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-done:
			return
		}
	}
}

func (s *Server) sendInitialSnapshots(conn *websocket.Conn, keys map[domain.BookKey]struct{}) {
	for _, k := range s.registry.Keys() {
		if keys != nil {
			if _, ok := keys[k]; !ok {
				continue
			}
		}
		bk, ok := s.registry.Get(k)
		if !ok {
			continue
		}
		snap := bk.Snapshot()
		if snap.LastUpdatedAt.IsZero() {
			continue
		}
		msg := bookMessage{
			Type:      "snapshot",
			Exchange:  snap.Exchange,
			Symbol:    string(snap.Symbol),
			Bids:      snap.Bids,
			Asks:      snap.Asks,
			Timestamp: snap.LastUpdatedAt.Format(time.RFC3339Nano),
		}
		if err := writeJSON(conn, msg, s.cfg.WriteTimeout); err != nil {
			slog.Info("server: snapshot send failed", "key", k, "error", err)
			return
		}
	}
}

func writeJSON(conn *websocket.Conn, msg bookMessage, timeout time.Duration) error {
	conn.SetWriteDeadline(time.Now().Add(timeout))
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

// runs the downstream ws server - accepts client connections - send book snapshots on initial connect and then streams incremental delta updates as they arrive from the pipeline

package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
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

// what the hub sends to a client goroutine
type notification struct {
	Symbol domain.Symbol // ! WHY some naming simple / some capital
	isFull bool          // true if the update is a full snapshot, false if it's a delta update
}

// sent by a new client to the hub
type registration struct {
	symbols  map[domain.Symbol]struct{} // symbols the client is interested in - "nil" means all symbols
	notifyCh chan<- notification
	done     <-chan struct{} // to signal the hub that the client has disconnected
}

type Config struct {
	Addr         string
	WriteTimeout time.Duration
	PingInterval time.Duration
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
		register: make(chan registration, 32), // ! 32 clients for now - make this configurable
		changed:  changed,
	}
}

func (s *Server) Run() {}

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

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"example/hello/internal/indexer"
	"example/hello/internal/storage"
	"example/hello/pkg/types"

	"github.com/gorilla/websocket"
)

// Server handles HTTP API endpoints
type Server struct {
	indexer *indexer.Indexer
	storage storage.Storage
	logger  *slog.Logger
	addr    string
	mux     *http.ServeMux
}

// NewServer creates a new API server
func NewServer(idx *indexer.Indexer, store storage.Storage, logger *slog.Logger, addr string) *Server {
	s := &Server{
		indexer: idx,
		storage: store,
		logger:  logger,
		addr:    addr,
		mux:     http.NewServeMux(),
	}
	s.registerRoutes()
	return s
}

// registerRoutes sets up all HTTP routes
func (s *Server) registerRoutes() {
	// Health check
	s.mux.HandleFunc("/v1/health", s.handleHealth)

	// Status/stats
	s.mux.HandleFunc("/v1/status", s.handleStatus)

	// Logs endpoints
	s.mux.HandleFunc("/v1/logs", s.handleGetLogs)
	s.mux.HandleFunc("/v1/logs/", s.handleLogQuery)

	// WebSocket for live updates
	s.mux.HandleFunc("/v1/ws", s.handleWebSocket)

	// Prometheus metrics
	s.mux.HandleFunc("/metrics", s.handleMetrics)

	// Legacy compatibility
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/stats", s.handleStatus)
}

// handleHealth returns health status
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	stats, err := s.indexer.GetStats(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get stats")
		return
	}

	status := "healthy"
	if stats.HeadLag > 128 {
		status = "lagging"
	}

	health := &types.HealthStatus{
		Status:           status,
		Timestamp:        time.Now().Unix(),
		LastBlockIndexed: stats.LastBlockNumber,
		TotalIndexed:     stats.TotalIndexed,
		HeadLag:          stats.HeadLag,
	}

	writeJSON(w, health)
}

// handleStatus returns detailed indexer status
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	stats, err := s.indexer.GetStats(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get stats")
		return
	}

	writeJSON(w, stats)
}

// handleGetLogs retrieves logs by query parameters
func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	q := r.URL.Query()
	startIndex := parseUint64(q.Get("startIndex"), 0)
	endIndex := parseUint64(q.Get("endIndex"), 0)
	blockNumber := parseUint64(q.Get("blockNumber"), 0)
	txHash := q.Get("txHash")
	limit := parseInt(q.Get("limit"), 100)

	var logs []*types.LogEntry
	var err error

	switch {
	case blockNumber > 0:
		logs, err = s.storage.GetLogsByBlockNumber(ctx, blockNumber)
	case txHash != "":
		logs, err = s.storage.GetLogsByTxHash(ctx, txHash)
	default:
		if startIndex == 0 && endIndex == 0 && limit > 0 {
			// Get latest N logs
			total, _ := s.storage.GetTotalCount(ctx)
			if total > 0 {
				startIndex = total - uint64(limit)
				endIndex = total - 1
			}
		}
		logs, err = s.storage.GetLogsByRange(ctx, startIndex, endIndex, limit)
	}

	if err != nil && err.Error() != "not found" {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Query failed: %v", err))
		return
	}

	if logs == nil {
		logs = make([]*types.LogEntry, 0)
	}

	writeJSON(w, logs)
}

// handleLogQuery handles queries for specific log indices or ranges
func (s *Server) handleLogQuery(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Extract index from path: /v1/logs/{index}
	indexStr := r.URL.Path[len("/v1/logs/"):]
	if indexStr == "" {
		http.Redirect(w, r, "/v1/logs", http.StatusMovedPermanently)
		return
	}

	index, err := strconv.ParseUint(indexStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid index")
		return
	}

	log, err := s.storage.GetLog(ctx, index)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Log not found: %v", err))
		return
	}

	writeJSON(w, log)
}

// handleWebSocket upgrades to WebSocket and streams live logs
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("WebSocket upgrade failed", "err", err)
		return
	}
	defer conn.Close()

	// Send welcome message
	conn.WriteJSON(map[string]interface{}{
		"type":    "welcome",
		"message": "Connected to live log stream",
	})

	liveCh := s.indexer.GetLiveChannel()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case entry := <-liveCh:
			if err := conn.WriteJSON(map[string]interface{}{
				"type": "log",
				"data": entry,
			}); err != nil {
				return
			}
		case <-ticker.C:
			// Ping to keep connection alive
			if err := conn.WriteJSON(map[string]interface{}{
				"type": "ping",
			}); err != nil {
				return
			}
		}
	}
}

// handleMetrics serves Prometheus metrics in text format
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	// This would be handled by Prometheus client library
	// For now, we'll return a simple response
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("# HELP eth_indexer_logs_indexed_total Total number of logs indexed\n"))
	w.Write([]byte("# TYPE eth_indexer_logs_indexed_total counter\n"))
}

// Helper functions

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	resp := types.ApiResponse{
		Status: statusCode,
		Error:  message,
	}
	json.NewEncoder(w).Encode(resp)
}

func parseUint64(s string, defaultVal uint64) uint64 {
	if s == "" {
		return defaultVal
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return defaultVal
	}
	return v
}

func parseInt(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return v
}

// Start starts the HTTP server
func (s *Server) Start() error {
	server := &http.Server{
		Addr:         s.addr,
		Handler:      s.mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	s.logger.Info("API server starting", "addr", s.addr)
	return server.ListenAndServe()
}

// StartWithContext starts the server and handles graceful shutdown
func (s *Server) StartWithContext(ctx context.Context) error {
	server := &http.Server{
		Addr:         s.addr,
		Handler:      s.mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	s.logger.Info("API server starting", "addr", s.addr)
	err := server.ListenAndServe()
	if err != http.ErrServerClosed {
		return err
	}
	return nil
}

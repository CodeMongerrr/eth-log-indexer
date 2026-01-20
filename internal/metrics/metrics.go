package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for the indexer
type Metrics struct {
	LogsIndexedTotal  prometheus.Counter
	RPCErrorsTotal    prometheus.Counter
	RPCLatencySeconds prometheus.Histogram
	HeadLagBlocks     prometheus.Gauge
	BackfillProgress  prometheus.Gauge
	LastBlockHeight   prometheus.Gauge
	StorageKeysTotal  prometheus.Gauge
	ReorgsDetected    prometheus.Counter
	BlocksRolledBack  prometheus.Counter
	CheckpointsSaved  prometheus.Counter
}

// NewMetrics creates and registers all Prometheus metrics
func NewMetrics() *Metrics {
	return &Metrics{
		LogsIndexedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "eth_indexer_logs_indexed_total",
			Help: "Total number of log events indexed",
		}),
		RPCErrorsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "eth_indexer_rpc_errors_total",
			Help: "Total number of RPC errors encountered",
		}),
		RPCLatencySeconds: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "eth_indexer_rpc_latency_seconds",
			Help:    "RPC call latency in seconds",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10},
		}),
		HeadLagBlocks: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "eth_indexer_head_lag_blocks",
			Help: "Number of blocks behind the current head",
		}),
		BackfillProgress: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "eth_indexer_backfill_progress",
			Help: "Backfill progress as a percentage (0-100)",
		}),
		LastBlockHeight: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "eth_indexer_last_block_height",
			Help: "Height of the last indexed block",
		}),
		StorageKeysTotal: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "eth_indexer_storage_keys_total",
			Help: "Total number of keys in storage",
		}),
		ReorgsDetected: promauto.NewCounter(prometheus.CounterOpts{
			Name: "eth_indexer_reorgs_detected_total",
			Help: "Total number of chain reorgs detected",
		}),
		BlocksRolledBack: promauto.NewCounter(prometheus.CounterOpts{
			Name: "eth_indexer_blocks_rolled_back_total",
			Help: "Total number of blocks rolled back due to reorgs",
		}),
		CheckpointsSaved: promauto.NewCounter(prometheus.CounterOpts{
			Name: "eth_indexer_checkpoints_saved_total",
			Help: "Total number of checkpoints saved",
		}),
	}
}

// RecordLogIndexed records a log event being indexed
func (m *Metrics) RecordLogIndexed() {
	m.LogsIndexedTotal.Inc()
}

// RecordRPCError records an RPC error
func (m *Metrics) RecordRPCError() {
	m.RPCErrorsTotal.Inc()
}

// RecordRPCLatency records RPC call latency
func (m *Metrics) RecordRPCLatency(seconds float64) {
	m.RPCLatencySeconds.Observe(seconds)
}

// SetHeadLag sets the current head lag
func (m *Metrics) SetHeadLag(blocks uint64) {
	m.HeadLagBlocks.Set(float64(blocks))
}

// SetBackfillProgress sets the backfill progress percentage
func (m *Metrics) SetBackfillProgress(progress float64) {
	m.BackfillProgress.Set(progress)
}

// SetLastBlockHeight sets the last indexed block height
func (m *Metrics) SetLastBlockHeight(height uint64) {
	m.LastBlockHeight.Set(float64(height))
}

// SetStorageKeysTotal sets the total number of storage keys
func (m *Metrics) SetStorageKeysTotal(count uint64) {
	m.StorageKeysTotal.Set(float64(count))
}

// RecordReorgDetected records a chain reorganization detection
func (m *Metrics) RecordReorgDetected() {
	m.ReorgsDetected.Inc()
}

// RecordBlocksRolledBack records blocks being rolled back
func (m *Metrics) RecordBlocksRolledBack(count uint64) {
	m.BlocksRolledBack.Add(float64(count))
}

// RecordCheckpointSaved records a checkpoint being saved
func (m *Metrics) RecordCheckpointSaved() {
	m.CheckpointsSaved.Inc()
}

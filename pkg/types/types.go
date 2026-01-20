package types

import "time"

// LogEntry represents an indexed Ethereum log event
type LogEntry struct {
	Index       uint64    `json:"index"`
	BlockNumber uint64    `json:"blockNumber"`
	BlockHash   string    `json:"blockHash"`
	ParentHash  string    `json:"parentHash"`
	L1InfoRoot  string    `json:"l1InfoRoot"`
	Timestamp   uint64    `json:"timestamp"`
	GasUsed     uint64    `json:"gasUsed"`
	TxHash      string    `json:"txHash"`
	LogIndex    uint64    `json:"logIndex"`
	CreatedAt   time.Time `json:"createdAt"`
}

// CheckpointData represents the cursor state for resuming indexing
type CheckpointData struct {
	LastProcessedBlock uint64 `json:"lastProcessedBlock"`
	NextIndex          uint64 `json:"nextIndex"`
	LastBlockHash      string `json:"lastBlockHash"`
	Timestamp          int64  `json:"timestamp"`
}

// RollbackInfo tracks reorg detection and rollback actions
type RollbackInfo struct {
	DetectedAt      time.Time `json:"detectedAt"`
	RolledBackCount uint64    `json:"rolledBackCount"`
	Reason          string    `json:"reason"`
}

// ApiResponse wraps API responses
type ApiResponse struct {
	Status  int         `json:"status"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// HealthStatus represents the health check response
type HealthStatus struct {
	Status           string `json:"status"`
	Timestamp        int64  `json:"timestamp"`
	LastBlockIndexed uint64 `json:"lastBlockIndexed"`
	TotalIndexed     uint64 `json:"totalIndexed"`
	HeadLag          uint64 `json:"headLag"`
}

// IndexerStats represents current indexer statistics
type IndexerStats struct {
	TotalIndexed     uint64        `json:"totalIndexed"`
	Processed        int64         `json:"processed"`
	NextIndex        uint64        `json:"nextIndex"`
	LastBlockNumber  uint64        `json:"lastBlockNumber"`
	LastBlockHash    string        `json:"lastBlockHash"`
	HeadBlock        uint64        `json:"headBlock"`
	HeadLag          uint64        `json:"headLag"`
	BackfillProgress float64       `json:"backfillProgress"`
	RPCErrors        int64         `json:"rpcErrors"`
	LastRollback     *RollbackInfo `json:"lastRollback,omitempty"`
}

// LogsQueryRequest represents query parameters for log retrieval
type LogsQueryRequest struct {
	StartIndex  uint64 `json:"startIndex,omitempty"`
	EndIndex    uint64 `json:"endIndex,omitempty"`
	BlockNumber uint64 `json:"blockNumber,omitempty"`
	TxHash      string `json:"txHash,omitempty"`
	Limit       int    `json:"limit,omitempty"`
	Offset      int    `json:"offset,omitempty"`
}

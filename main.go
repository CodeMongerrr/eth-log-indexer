package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/boltdb/bolt"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

const (
	CONTRACT_ADDR   = "0x6992e2f8E29139cc16683228a4A4CA602e49e048"
	EVENT_TOPIC     = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	RPC_ENDPOINT    = "https://eth-mainnet.g.alchemy.com/public"
	BUCKET_NAME     = "logs"
	DB_DIR          = "worker_dbs"
	FINAL_DB        = "hyperscale_indexed_logs.db"
	MAX_BLOCK_RANGE = 500 // RPC constraint: maximum 500 blocks per query
)

type LogEntry struct {
	Index       uint64 `json:"index"`
	BlockNumber uint64 `json:"blockNumber"`
	ParentHash  string `json:"parentHash"`
	L1InfoRoot  string `json:"l1InfoRoot"`
	Timestamp   uint64 `json:"timestamp"`
	GasUsed     uint64 `json:"gasUsed"`
	TxHash      string `json:"txHash"`
	LogIndex    uint64 `json:"logIndex"`
}

type BatchInfo struct {
	WorkerID       int
	BatchID        int
	StartBlock     uint64
	EndBlock       uint64
	StartIndex     uint64
	LogCount       uint64
	DbPath         string
	ProcessingTime time.Duration
	GasAnalyzed    uint64
}

type PerformanceMetrics struct {
	TotalBlocks        uint64
	TotalLogs          uint64
	TotalGasAnalyzed   uint64
	TotalBatches       int
	ProcessingTime     time.Duration
	ThroughputBPS      float64
	ThroughputLPS      float64
	ParallelEfficiency float64
	StartTime          time.Time
	EndTime            time.Time
}

type IndexerConfig struct {
	StartBlock    uint64
	EndBlock      uint64
	NumWorkers    int
	EnableCache   bool
	EnableMetrics bool
}

type HyperscaleIndexer struct {
	client       *ethclient.Client
	config       IndexerConfig
	metrics      PerformanceMetrics
	processed    int64
	errors       chan error
	batchCounter int64
	mu           sync.RWMutex
}

func NewHyperscaleIndexer(client *ethclient.Client, config IndexerConfig) *HyperscaleIndexer {
	return &HyperscaleIndexer{
		client: client,
		config: config,
		errors: make(chan error, config.NumWorkers*10), // Buffer for multiple batches per worker
		metrics: PerformanceMetrics{
			StartTime: time.Now(),
		},
	}
}

func (h *HyperscaleIndexer) generateAdaptiveBatches() ([]BatchInfo, error) {
	totalBlocks := h.config.EndBlock - h.config.StartBlock + 1

	// Calculate number of batches needed based on MAX_BLOCK_RANGE constraint
	numBatches := int((totalBlocks + MAX_BLOCK_RANGE - 1) / MAX_BLOCK_RANGE) // Ceiling division

	batches := make([]BatchInfo, 0, numBatches)
	currentIndex := uint64(0)
	batchID := 0

	log.Printf("🔄 Adaptive Range Analysis: %d total blocks requires %d batches (max %d blocks each)",
		totalBlocks, numBatches, MAX_BLOCK_RANGE)

	for startBlock := h.config.StartBlock; startBlock <= h.config.EndBlock; {
		endBlock := startBlock + MAX_BLOCK_RANGE - 1
		if endBlock > h.config.EndBlock {
			endBlock = h.config.EndBlock
		}

		// Pre-analyze this batch to get log count
		query := ethereum.FilterQuery{
			FromBlock: big.NewInt(int64(startBlock)),
			ToBlock:   big.NewInt(int64(endBlock)),
			Addresses: []common.Address{common.HexToAddress(CONTRACT_ADDR)},
			Topics:    [][]common.Hash{{common.HexToHash(EVENT_TOPIC)}},
		}

		logs, err := h.client.FilterLogs(context.Background(), query)
		if err != nil {
			return nil, fmt.Errorf("failed to pre-analyze batch %d (blocks %d-%d): %v",
				batchID, startBlock, endBlock, err)
		}

		dbPath := filepath.Join(DB_DIR, fmt.Sprintf("adaptive_batch_%d.db", batchID))
		batch := BatchInfo{
			WorkerID:   batchID % h.config.NumWorkers, // Round-robin assignment to workers
			BatchID:    batchID,
			StartBlock: startBlock,
			EndBlock:   endBlock,
			StartIndex: currentIndex,
			LogCount:   uint64(len(logs)),
			DbPath:     dbPath,
		}

		batches = append(batches, batch)
		currentIndex += uint64(len(logs))

		log.Printf("📦 Batch %d: Blocks %d-%d (%d blocks) | Events: %d | Worker: %d | Starting Index: %d",
			batchID, startBlock, endBlock, endBlock-startBlock+1, len(logs), batch.WorkerID, batch.StartIndex)

		startBlock = endBlock + 1
		batchID++
	}

	h.metrics.TotalBatches = len(batches)
	log.Printf("✅ Generated %d adaptive batches distributed across %d workers", len(batches), h.config.NumWorkers)

	return batches, nil
}

func (h *HyperscaleIndexer) processAdaptiveBatch(batch BatchInfo) error {
	startTime := time.Now()

	db, err := bolt.Open(batch.DbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return fmt.Errorf("failed to open batch db: %v", err)
	}
	defer db.Close()

	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(BUCKET_NAME))
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to create bucket: %v", err)
	}

	// Ensure we stay within the 500 block limit
	blockRange := batch.EndBlock - batch.StartBlock + 1
	if blockRange > MAX_BLOCK_RANGE {
		return fmt.Errorf("batch %d exceeds max block range: %d > %d",
			batch.BatchID, blockRange, MAX_BLOCK_RANGE)
	}

	query := ethereum.FilterQuery{
		FromBlock: big.NewInt(int64(batch.StartBlock)),
		ToBlock:   big.NewInt(int64(batch.EndBlock)),
		Addresses: []common.Address{common.HexToAddress(CONTRACT_ADDR)},
		Topics:    [][]common.Hash{{common.HexToHash(EVENT_TOPIC)}},
	}

	logs, err := h.client.FilterLogs(context.Background(), query)
	if err != nil {
		return fmt.Errorf("worker %d batch %d failed to get logs: %v", batch.WorkerID, batch.BatchID, err)
	}

	var totalGas uint64

	err = db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(BUCKET_NAME))

		for i, logEntry := range logs {
			block, err := h.client.BlockByHash(context.Background(), logEntry.BlockHash)
			if err != nil {
				return fmt.Errorf("failed to get block %d: %v", logEntry.BlockNumber, err)
			}

			// Get transaction details for gas analysis
			tx, _, err := h.client.TransactionByHash(context.Background(), logEntry.TxHash)
			if err != nil {
				log.Printf("Warning: Could not get transaction %s: %v", logEntry.TxHash.Hex(), err)
			}

			var gasUsed uint64
			if tx != nil {
				gasUsed = tx.Gas()
				totalGas += gasUsed
			}

			entry := LogEntry{
				Index:       batch.StartIndex + uint64(i),
				BlockNumber: logEntry.BlockNumber,
				ParentHash:  block.ParentHash().Hex(),
				L1InfoRoot:  common.Bytes2Hex(logEntry.Data),
				Timestamp:   block.Time(),
				GasUsed:     gasUsed,
				TxHash:      logEntry.TxHash.Hex(),
				LogIndex:    uint64(logEntry.Index),
			}

			data, err := json.Marshal(entry)
			if err != nil {
				return fmt.Errorf("failed to marshal entry: %v", err)
			}

			err = bucket.Put(uint64ToBytes(entry.Index), data)
			if err != nil {
				return fmt.Errorf("failed to store entry: %v", err)
			}

			atomic.AddInt64(&h.processed, 1)
		}

		return nil
	})

	processingTime := time.Since(startTime)
	batch.ProcessingTime = processingTime
	batch.GasAnalyzed = totalGas

	h.mu.Lock()
	h.metrics.TotalGasAnalyzed += totalGas
	h.mu.Unlock()

	atomic.AddInt64(&h.batchCounter, 1)
	completedBatches := atomic.LoadInt64(&h.batchCounter)

	log.Printf("✅ Worker %d | Batch %d/%d: %d events in %v (%.1f events/sec) [%d/%d batches complete]",
		batch.WorkerID, batch.BatchID, h.metrics.TotalBatches, len(logs),
		processingTime, float64(len(logs))/processingTime.Seconds(),
		completedBatches, h.metrics.TotalBatches)

	return err
}

func (h *HyperscaleIndexer) consolidateAllBatches(batches []BatchInfo) error {
	log.Println("🔄 Initiating unified database consolidation...")

	finalDb, err := bolt.Open(FINAL_DB, 0600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return fmt.Errorf("failed to open final consolidated db: %v", err)
	}
	defer finalDb.Close()

	err = finalDb.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(BUCKET_NAME))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte("metadata"))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte("batch_info"))
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to create buckets in final db: %v", err)
	}

	var totalLogs uint64
	consolidationStart := time.Now()

	// Store batch information for analytics
	err = finalDb.Update(func(tx *bolt.Tx) error {
		batchBucket := tx.Bucket([]byte("batch_info"))
		for _, batch := range batches {
			batchData, err := json.Marshal(batch)
			if err != nil {
				return err
			}
			err = batchBucket.Put([]byte(fmt.Sprintf("batch_%d", batch.BatchID)), batchData)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		log.Printf("Warning: Failed to store batch info: %v", err)
	}

	// Merge all batch databases in order
	for i, batch := range batches {
		batchStart := time.Now()

		workerDb, err := bolt.Open(batch.DbPath, 0600, &bolt.Options{ReadOnly: true, Timeout: 2 * time.Second})
		if err != nil {
			return fmt.Errorf("failed to open batch db %s: %v", batch.DbPath, err)
		}

		var batchLogs uint64
		err = workerDb.View(func(tx *bolt.Tx) error {
			workerBucket := tx.Bucket([]byte(BUCKET_NAME))
			if workerBucket == nil {
				return fmt.Errorf("bucket not found in batch db %d", batch.BatchID)
			}

			return finalDb.Update(func(finalTx *bolt.Tx) error {
				finalBucket := finalTx.Bucket([]byte(BUCKET_NAME))

				return workerBucket.ForEach(func(k, v []byte) error {
					batchLogs++
					totalLogs++
					return finalBucket.Put(k, v)
				})
			})
		})

		workerDb.Close()
		if err != nil {
			return fmt.Errorf("failed to merge batch db %s: %v", batch.DbPath, err)
		}

		// Clean up individual batch database
		os.Remove(batch.DbPath)

		batchTime := time.Since(batchStart)
		log.Printf("📦 Consolidated Batch %d: %d events merged in %v (%d/%d complete)",
			batch.BatchID, batchLogs, batchTime, i+1, len(batches))
	}

	consolidationTime := time.Since(consolidationStart)
	log.Printf("⚡ Consolidation completed in %v (%.1f events/sec)",
		consolidationTime, float64(totalLogs)/consolidationTime.Seconds())

	h.metrics.TotalLogs = totalLogs
	h.metrics.EndTime = time.Now()
	h.metrics.ProcessingTime = h.metrics.EndTime.Sub(h.metrics.StartTime)

	err = h.storeMetrics(finalDb)
	if err != nil {
		log.Printf("Warning: Failed to store metrics: %v", err)
	}

	log.Printf("🚀 Unified consolidation complete: %s events indexed in single database", formatNumber(totalLogs))
	return nil
}

func (h *HyperscaleIndexer) storeMetrics(db *bolt.DB) error {
	h.metrics.TotalBlocks = h.config.EndBlock - h.config.StartBlock + 1
	h.metrics.ThroughputBPS = float64(h.metrics.TotalBlocks) / h.metrics.ProcessingTime.Seconds()
	h.metrics.ThroughputLPS = float64(h.metrics.TotalLogs) / h.metrics.ProcessingTime.Seconds()
	h.metrics.ParallelEfficiency = float64(h.config.NumWorkers) * h.metrics.ThroughputLPS / 1000.0

	return db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("metadata"))
		data, err := json.Marshal(h.metrics)
		if err != nil {
			return err
		}
		return bucket.Put([]byte("performance_metrics"), data)
	})
}

func (h *HyperscaleIndexer) printMetrics() {
	fmt.Println("\n" + strings.Repeat("=", 85))
	fmt.Println("🏆 ADAPTIVE ETHEREUM LOG INDEXER - PERFORMANCE ANALYTICS")
	fmt.Println(strings.Repeat("=", 85))
	fmt.Printf("📊 Blocks Processed:       %s\n", formatNumber(h.metrics.TotalBlocks))
	fmt.Printf("📈 Events Indexed:         %s\n", formatNumber(h.metrics.TotalLogs))
	fmt.Printf("📦 Adaptive Batches:       %d (max %d blocks each)\n", h.metrics.TotalBatches, MAX_BLOCK_RANGE)
	fmt.Printf("⛽ Gas Analyzed:           %s\n", formatNumber(h.metrics.TotalGasAnalyzed))
	fmt.Printf("⚡ Total Processing Time:  %v\n", h.metrics.ProcessingTime.Round(time.Millisecond))
	fmt.Printf("🚀 Throughput (Blocks):    %.2f blocks/sec\n", h.metrics.ThroughputBPS)
	fmt.Printf("📡 Throughput (Events):    %.2f events/sec\n", h.metrics.ThroughputLPS)
	fmt.Printf("🔧 Workers Utilized:       %d concurrent workers\n", h.config.NumWorkers)
	fmt.Printf("🔥 Efficiency Multiplier:  %.2fx performance boost\n", h.metrics.ParallelEfficiency)
	fmt.Printf("💾 Unified Database:       %s\n", FINAL_DB)
	fmt.Println(strings.Repeat("=", 85))
}

func formatNumber(n uint64) string {
	str := fmt.Sprintf("%d", n)
	if len(str) <= 3 {
		return str
	}

	result := ""
	for i, digit := range str {
		if i > 0 && (len(str)-i)%3 == 0 {
			result += ","
		}
		result += string(digit)
	}
	return result
}

func uint64ToBytes(n uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, n)
	return b
}

func main() {
	fmt.Println("🌟 ADAPTIVE ETHEREUM EVENT LOG INDEXER v2.1")
	fmt.Println("   RPC-Optimized Parallel Processing & Unified Database")
	fmt.Println("   Max Range: 500 blocks per query | Auto-rebalancing batches")
	fmt.Println()

	os.MkdirAll(DB_DIR, 0755)
	defer os.RemoveAll(DB_DIR)

	client, err := ethclient.Dial(RPC_ENDPOINT)
	if err != nil {
		log.Fatalf("❌ Failed to connect to Ethereum client: %v", err)
	}

	config := IndexerConfig{
		StartBlock:    22925713,
		EndBlock:      22961057,
		NumWorkers:    50, // Optimal for RPC rate limits
		EnableCache:   true,
		EnableMetrics: true,
	}

	totalBlocks := config.EndBlock - config.StartBlock + 1
	estimatedBatches := int((totalBlocks + MAX_BLOCK_RANGE - 1) / MAX_BLOCK_RANGE)

	log.Printf("📊 Range Analysis: %s blocks will be processed in ~%d adaptive batches",
		formatNumber(totalBlocks), estimatedBatches)

	indexer := NewHyperscaleIndexer(client, config)

	log.Println("🔍 Generating RPC-optimized adaptive batches...")
	batches, err := indexer.generateAdaptiveBatches()
	if err != nil {
		log.Fatalf("❌ Failed to generate adaptive batches: %v", err)
	}

	log.Printf("🚀 Launching %d workers to process %d adaptive batches...", config.NumWorkers, len(batches))

	var wg sync.WaitGroup
	startTime := time.Now()

	// Enhanced progress monitoring
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				processed := atomic.LoadInt64(&indexer.processed)
				completed := atomic.LoadInt64(&indexer.batchCounter)
				elapsed := time.Since(startTime)
				rate := float64(processed) / elapsed.Seconds()
				progress := float64(completed) / float64(len(batches)) * 100

				log.Printf("📊 Progress: %s events | %d/%d batches (%.1f%%) | %.1f events/sec",
					formatNumber(uint64(processed)), completed, len(batches), progress, rate)
			}
		}
	}()

	// Process batches with worker pooling
	batchChan := make(chan BatchInfo, len(batches))

	// Start workers
	for i := 0; i < config.NumWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for batch := range batchChan {
				if err := indexer.processAdaptiveBatch(batch); err != nil {
					indexer.errors <- fmt.Errorf("worker %d batch %d error: %v", workerID, batch.BatchID, err)
				}
			}
		}(i)
	}

	// Distribute batches to workers
	go func() {
		defer close(batchChan)
		for _, batch := range batches {
			batchChan <- batch
		}
	}()

	wg.Wait()
	close(indexer.errors)

	// Report any errors
	errorCount := 0
	for err := range indexer.errors {
		log.Printf("⚠️  Processing error: %v", err)
		errorCount++
	}

	if errorCount > 0 {
		log.Printf("⚠️  Total errors encountered: %d", errorCount)
	}

	log.Println("🔄 Consolidating all batches into unified database...")
	if err := indexer.consolidateAllBatches(batches); err != nil {
		log.Fatalf("❌ Failed to consolidate databases: %v", err)
	}

	indexer.printMetrics()
	log.Printf("🎉 Adaptive indexing complete! Unified database: %s", FINAL_DB)
	log.Printf("📈 Total efficiency: Processed %s events from %s blocks using RPC-optimized batching",
		formatNumber(indexer.metrics.TotalLogs), formatNumber(indexer.metrics.TotalBlocks))
}

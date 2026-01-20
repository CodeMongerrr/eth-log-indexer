package storage

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"

	"example/hello/pkg/types"

	bolt "github.com/boltdb/bolt"
)

const (
	BucketLogs       = "logs"
	BucketMeta       = "meta"
	BucketCheckpoint = "checkpoint"
	BucketBlockMap   = "blockmap" // maps block hash to index
)

// KeyLastBlock stores the last processed block number
const KeyLastBlock = "lastBlock"

// KeyNextIndex stores the next index to assign
const KeyNextIndex = "nextIndex"

// KeyLastBlockHash stores the hash of the last processed block
const KeyLastBlockHash = "lastBlockHash"

// Storage defines the interface for persistent storage
type Storage interface {
	StoreLog(ctx context.Context, entry *types.LogEntry) error
	GetLog(ctx context.Context, index uint64) (*types.LogEntry, error)
	GetLogsByRange(ctx context.Context, startIndex, endIndex uint64, limit int) ([]*types.LogEntry, error)
	GetLogsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*types.LogEntry, error)
	GetLogsByTxHash(ctx context.Context, txHash string) ([]*types.LogEntry, error)
	GetLastIndex(ctx context.Context) (uint64, error)
	GetTotalCount(ctx context.Context) (uint64, error)
	SaveCheckpoint(ctx context.Context, checkpoint *types.CheckpointData) error
	GetCheckpoint(ctx context.Context) (*types.CheckpointData, error)
	StoreBlockHash(ctx context.Context, blockNumber uint64, blockHash string) error
	GetBlockHash(ctx context.Context, blockNumber uint64) (string, error)
	Rollback(ctx context.Context, toBlockNumber uint64) error
	Close() error
}

// BoltStorage implements Storage using BoltDB
type BoltStorage struct {
	db *bolt.DB
	mu sync.RWMutex
}

// NewBoltStorage creates a new BoltDB storage instance
func NewBoltStorage(dbPath string) (*BoltStorage, error) {
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 0})
	if err != nil {
		return nil, fmt.Errorf("failed to open boltdb: %w", err)
	}

	// Create required buckets
	err = db.Update(func(tx *bolt.Tx) error {
		for _, bucket := range []string{BucketLogs, BucketMeta, BucketCheckpoint, BucketBlockMap} {
			if _, e := tx.CreateBucketIfNotExists([]byte(bucket)); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create buckets: %w", err)
	}

	return &BoltStorage{db: db}, nil
}

// StoreLog persists a log entry
func (s *BoltStorage) StoreLog(ctx context.Context, entry *types.LogEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	val, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal log: %w", err)
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(BucketLogs))
		if b == nil {
			return fmt.Errorf("logs bucket missing")
		}
		return b.Put(uint64ToBytes(entry.Index), val)
	})
}

// GetLog retrieves a single log by index
func (s *BoltStorage) GetLog(ctx context.Context, index uint64) (*types.LogEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var entry types.LogEntry
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(BucketLogs))
		if b == nil {
			return fmt.Errorf("logs bucket missing")
		}
		v := b.Get(uint64ToBytes(index))
		if v == nil {
			return fmt.Errorf("not found")
		}
		return json.Unmarshal(v, &entry)
	})
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

// GetLogsByRange retrieves logs within a range of indices
func (s *BoltStorage) GetLogsByRange(ctx context.Context, startIndex, endIndex uint64, limit int) ([]*types.LogEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]*types.LogEntry, 0, 64)
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(BucketLogs))
		if b == nil {
			return fmt.Errorf("logs bucket missing")
		}
		c := b.Cursor()
		minKey := uint64ToBytes(startIndex)
		for k, v := c.Seek(minKey); k != nil; k, v = c.Next() {
			idx := bytesToUint64(k)
			if endIndex > 0 && idx > endIndex {
				break
			}
			var le types.LogEntry
			if err := json.Unmarshal(v, &le); err != nil {
				return err
			}
			results = append(results, &le)
			if limit > 0 && len(results) >= limit {
				break
			}
		}
		return nil
	})
	return results, err
}

// GetLogsByBlockNumber retrieves all logs for a specific block
func (s *BoltStorage) GetLogsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*types.LogEntry, error) {
	// For BoltDB without secondary indexes, we scan all logs
	// In a production system, use a database with proper indexing
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]*types.LogEntry, 0)
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(BucketLogs))
		if b == nil {
			return nil
		}
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var le types.LogEntry
			if err := json.Unmarshal(v, &le); err != nil {
				continue
			}
			if le.BlockNumber == blockNumber {
				results = append(results, &le)
			}
		}
		return nil
	})
	return results, err
}

// GetLogsByTxHash retrieves all logs for a specific transaction
func (s *BoltStorage) GetLogsByTxHash(ctx context.Context, txHash string) ([]*types.LogEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]*types.LogEntry, 0)
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(BucketLogs))
		if b == nil {
			return nil
		}
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var le types.LogEntry
			if err := json.Unmarshal(v, &le); err != nil {
				continue
			}
			if le.TxHash == txHash {
				results = append(results, &le)
			}
		}
		return nil
	})
	return results, err
}

// GetLastIndex returns the next index to assign
func (s *BoltStorage) GetLastIndex(ctx context.Context) (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var last uint64 = 0
	s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(BucketLogs))
		if b == nil {
			return nil
		}
		c := b.Cursor()
		k, _ := c.Last()
		if k != nil {
			last = bytesToUint64(k) + 1
		}
		return nil
	})
	return last, nil
}

// GetTotalCount returns the total number of stored logs
func (s *BoltStorage) GetTotalCount(ctx context.Context) (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var cnt uint64 = 0
	s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(BucketLogs))
		if b == nil {
			return nil
		}
		stats := b.Stats()
		cnt = uint64(stats.KeyN)
		return nil
	})
	return cnt, nil
}

// SaveCheckpoint persists checkpoint data for resuming
func (s *BoltStorage) SaveCheckpoint(ctx context.Context, checkpoint *types.CheckpointData) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	val, err := json.Marshal(checkpoint)
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint: %w", err)
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(BucketCheckpoint))
		if b == nil {
			return fmt.Errorf("checkpoint bucket missing")
		}
		return b.Put([]byte("current"), val)
	})
}

// GetCheckpoint retrieves the latest checkpoint data
func (s *BoltStorage) GetCheckpoint(ctx context.Context) (*types.CheckpointData, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var checkpoint types.CheckpointData
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(BucketCheckpoint))
		if b == nil {
			return fmt.Errorf("checkpoint bucket missing")
		}
		v := b.Get([]byte("current"))
		if v == nil {
			return fmt.Errorf("no checkpoint found")
		}
		return json.Unmarshal(v, &checkpoint)
	})
	if err != nil {
		return nil, err
	}
	return &checkpoint, nil
}

// StoreBlockHash stores the block hash for a given block number
func (s *BoltStorage) StoreBlockHash(ctx context.Context, blockNumber uint64, blockHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(BucketBlockMap))
		if b == nil {
			return fmt.Errorf("blockmap bucket missing")
		}
		return b.Put(uint64ToBytes(blockNumber), []byte(blockHash))
	})
}

// GetBlockHash retrieves the block hash for a given block number
func (s *BoltStorage) GetBlockHash(ctx context.Context, blockNumber uint64) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var hash string
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(BucketBlockMap))
		if b == nil {
			return fmt.Errorf("blockmap bucket missing")
		}
		v := b.Get(uint64ToBytes(blockNumber))
		if v == nil {
			return fmt.Errorf("not found")
		}
		hash = string(v)
		return nil
	})
	return hash, err
}

// Rollback removes all logs from a given block number onwards
func (s *BoltStorage) Rollback(ctx context.Context, toBlockNumber uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(BucketLogs))
		if b == nil {
			return nil
		}

		var keysToDelete [][]byte
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var le types.LogEntry
			if err := json.Unmarshal(v, &le); err != nil {
				continue
			}
			if le.BlockNumber > toBlockNumber {
				keysToDelete = append(keysToDelete, k)
			}
		}

		for _, k := range keysToDelete {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}

// Close closes the BoltDB connection
func (s *BoltStorage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}

// Utility functions for uint64 conversion
func uint64ToBytes(n uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, n)
	return b
}

func bytesToUint64(b []byte) uint64 {
	return binary.BigEndian.Uint64(b)
}

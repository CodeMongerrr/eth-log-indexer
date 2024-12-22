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
    "sync"

    "github.com/ethereum/go-ethereum"
    "github.com/ethereum/go-ethereum/common"
    "github.com/ethereum/go-ethereum/ethclient"
    "github.com/boltdb/bolt"
)

const (
    CONTRACT_ADDR = "0xA13Ddb14437A8F34897131367ad3ca78416d6bCa"
    EVENT_TOPIC   = "0x3e54d0825ed78523037d00a81759237eb436ce774bd546993ee67a1b67b6e766"
	RPC_ENDPOINT  = "https://eth-sepolia.g.alchemy.com/v2/exekK53YRdHz42FMiwI6rkoIN45VTY7u"
    BUCKET_NAME   = "logs"
    DB_DIR        = "worker_dbs"
    FINAL_DB      = "final_logs.db"
)

type LogEntry struct {
    Index       uint64 `json:"index"`
    BlockNumber uint64 `json:"blockNumber"`
    ParentHash  string `json:"parentHash"`
    L1InfoRoot  string `json:"l1InfoRoot"`
}

type BatchInfo struct {
    WorkerID    int
    StartBlock  uint64
    EndBlock    uint64
    StartIndex  uint64
    LogCount    uint64
    DbPath      string
}

// Generate batches and calculate log counts
func generateBatches(client *ethclient.Client, startBlock, endBlock uint64, numBatches int) ([]BatchInfo, error) {
    totalBlocks := endBlock - startBlock + 1
    blocksPerBatch := totalBlocks / uint64(numBatches)
    
    batches := make([]BatchInfo, numBatches)
    currentIndex := uint64(0)

    for i := 0; i < numBatches; i++ {
        batchStart := startBlock + (uint64(i) * blocksPerBatch)
        batchEnd := batchStart + blocksPerBatch - 1
        if i == numBatches-1 {
            batchEnd = endBlock
        }

        query := ethereum.FilterQuery{
            FromBlock: big.NewInt(int64(batchStart)),
            ToBlock:   big.NewInt(int64(batchEnd)),
            Addresses: []common.Address{
                common.HexToAddress(CONTRACT_ADDR),
            },
            Topics: [][]common.Hash{{
                common.HexToHash(EVENT_TOPIC),
            }},
        }

        logs, err := client.FilterLogs(context.Background(), query)
        if err != nil {
            return nil, fmt.Errorf("failed to get logs for batch %d: %v", i, err)
        }

        dbPath := filepath.Join(DB_DIR, fmt.Sprintf("worker_%d.db", i))
        batches[i] = BatchInfo{
            WorkerID:    i,
            StartBlock:  batchStart,
            EndBlock:    batchEnd,
            StartIndex:  currentIndex,
            LogCount:    uint64(len(logs)),
            DbPath:      dbPath,
        }

        currentIndex += uint64(len(logs))
        log.Printf("Batch %d: Blocks %d-%d, Logs: %d, Starting Index: %d",
            i, batchStart, batchEnd, len(logs), batches[i].StartIndex)
    }

    return batches, nil
}

// Process a single batch with its own database
func processBatch(client *ethclient.Client, batch BatchInfo) error {
    db, err := bolt.Open(batch.DbPath, 0600, nil)
    if err != nil {
        return fmt.Errorf("failed to open worker db: %v", err)
    }
    defer db.Close()

    err = db.Update(func(tx *bolt.Tx) error {
        _, err := tx.CreateBucketIfNotExists([]byte(BUCKET_NAME))
        return err
    })
    if err != nil {
        return fmt.Errorf("failed to create bucket: %v", err)
    }

    query := ethereum.FilterQuery{
        FromBlock: big.NewInt(int64(batch.StartBlock)),
        ToBlock:   big.NewInt(int64(batch.EndBlock)),
        Addresses: []common.Address{
            common.HexToAddress(CONTRACT_ADDR),
        },
        Topics: [][]common.Hash{{
            common.HexToHash(EVENT_TOPIC),
        }},
    }

    logs, err := client.FilterLogs(context.Background(), query)
    if err != nil {
        return fmt.Errorf("failed to get logs: %v", err)
    }

    return db.Update(func(tx *bolt.Tx) error {
        bucket := tx.Bucket([]byte(BUCKET_NAME))
        
        for i, logEntry := range logs {
            block, err := client.BlockByHash(context.Background(), logEntry.BlockHash)
            if err != nil {
                return fmt.Errorf("failed to get block %d: %v", logEntry.BlockNumber, err)
            }

            entry := LogEntry{
                Index:       batch.StartIndex + uint64(i),
                BlockNumber: logEntry.BlockNumber,
                ParentHash:  block.ParentHash().Hex(),
                L1InfoRoot:  common.Bytes2Hex(logEntry.Data),
            }

            data, err := json.Marshal(entry)
            if err != nil {
                return fmt.Errorf("failed to marshal entry: %v", err)
            }

            err = bucket.Put(uint64ToBytes(entry.Index), data)
            if err != nil {
                return fmt.Errorf("failed to store entry: %v", err)
            }
        }

        return nil
    })
}

// Merge all worker databases into final database
func mergeDatabases(batches []BatchInfo) error {
    finalDb, err := bolt.Open(FINAL_DB, 0600, nil)
    if err != nil {
        return fmt.Errorf("failed to open final db: %v", err)
    }
    defer finalDb.Close()

    err = finalDb.Update(func(tx *bolt.Tx) error {
        _, err := tx.CreateBucketIfNotExists([]byte(BUCKET_NAME))
        return err
    })
    if err != nil {
        return fmt.Errorf("failed to create bucket in final db: %v", err)
    }

    for _, batch := range batches {
        workerDb, err := bolt.Open(batch.DbPath, 0600, nil)
        if err != nil {
            return fmt.Errorf("failed to open worker db %s: %v", batch.DbPath, err)
        }

        err = workerDb.View(func(tx *bolt.Tx) error {
            workerBucket := tx.Bucket([]byte(BUCKET_NAME))
            if workerBucket == nil {
                return fmt.Errorf("bucket not found in worker db")
            }

            return finalDb.Update(func(finalTx *bolt.Tx) error {
                finalBucket := finalTx.Bucket([]byte(BUCKET_NAME))
                
                return workerBucket.ForEach(func(k, v []byte) error {
                    return finalBucket.Put(k, v)
                })
            })
        })

        workerDb.Close()
        if err != nil {
            return fmt.Errorf("failed to merge worker db %s: %v", batch.DbPath, err)
        }

        os.Remove(batch.DbPath)
    }

    return nil
}

func uint64ToBytes(n uint64) []byte {
    b := make([]byte, 8)
    binary.BigEndian.PutUint64(b, n)
    return b
}

func main() {
    os.MkdirAll(DB_DIR, 0755)
    defer os.RemoveAll(DB_DIR) 

    client, err := ethclient.Dial(RPC_ENDPOINT)
    if err != nil {
        log.Fatalf("Failed to connect to Ethereum client: %v", err)
    }

    log.Println("Generating batches...")
    batches, err := generateBatches(
        client,
        5157692,  
        7304770,  
        50,       
    )
    if err != nil {
        log.Fatalf("Failed to generate batches: %v", err)
    }

    var wg sync.WaitGroup
    errors := make(chan error, len(batches))

    for _, batch := range batches {
        wg.Add(1)
        go func(b BatchInfo) {
            defer wg.Done()
            if err := processBatch(client, b); err != nil {
                errors <- fmt.Errorf("worker %d error: %v", b.WorkerID, err)
            }
        }(batch)
    }

    wg.Wait()
    close(errors)

    for err := range errors {
        log.Printf("Error during processing: %v", err)
    }

    log.Println("Merging databases...")
    if err := mergeDatabases(batches); err != nil {
        log.Fatalf("Failed to merge databases: %v", err)
    }

    log.Println("Processing complete. Final database:", FINAL_DB)
}
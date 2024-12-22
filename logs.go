package main

import (
    "encoding/binary"
    "encoding/json"
    "flag"
    "fmt"
    "log"

    "github.com/boltdb/bolt"
)

const (
    BUCKET_NAME = "logs"
    META_BUCKET = "metadata"
)

type LogEntry struct {
    Index       uint64 `json:"index"`
    BlockNumber uint64 `json:"blockNumber"`
    ParentHash  string `json:"parentHash"`
    L1InfoRoot  string `json:"l1InfoRoot"`
}

type QueryOptions struct {
    dbPath     string
    index      uint64
    startIndex uint64
    endIndex   uint64
    count      bool    
    latest     int
    format     string
}

func main() {
    opts := parseFlags()

    // Open database
    db, err := bolt.Open(opts.dbPath, 0600, nil)
    if err != nil {
        log.Fatalf("Failed to open database: %v", err)
    }
    defer db.Close()

    switch {
    case opts.index > 0:
        queryByIndex(db, opts.index)
    case opts.startIndex > 0 || opts.endIndex > 0:
        queryRange(db, opts.startIndex, opts.endIndex)
    case opts.latest > 0:
        queryLatest(db, opts.latest)
    case opts.count:    
        getTotalCount(db)
    default:
        fmt.Println("Please specify a query option. Use -h for help.")
    }
}

func parseFlags() QueryOptions {
    opts := QueryOptions{}

    flag.StringVar(&opts.dbPath, "db", "final_logs.db", "Path to the BoltDB database")
    flag.Uint64Var(&opts.index, "index", 0, "Query by specific index")
    flag.Uint64Var(&opts.startIndex, "start", 0, "Start index for range query")
    flag.Uint64Var(&opts.endIndex, "end", 0, "End index for range query")
    flag.IntVar(&opts.latest, "latest", 0, "Query latest N entries")
    flag.BoolVar(&opts.count, "count", false, "Get total count of entries")    
    flag.StringVar(&opts.format, "format", "text", "Output format (text/json)")

    flag.Parse()
    return opts
}

// Query a single entry by index
func queryByIndex(db *bolt.DB, index uint64) {
    var entry LogEntry

    err := db.View(func(tx *bolt.Tx) error {
        bucket := tx.Bucket([]byte(BUCKET_NAME))
        if bucket == nil {
            return fmt.Errorf("bucket not found")
        }

        data := bucket.Get(uint64ToBytes(index))
        if data == nil {
            return fmt.Errorf("no entry found for index %d", index)
        }

        return json.Unmarshal(data, &entry)
    })

    if err != nil {
        log.Fatalf("Error querying index %d: %v", index, err)
    }

    printEntry(entry)
}

// Query a range of entries
func queryRange(db *bolt.DB, start, end uint64) {
    var entries []LogEntry

    err := db.View(func(tx *bolt.Tx) error {
        bucket := tx.Bucket([]byte(BUCKET_NAME))
        if bucket == nil {
            return fmt.Errorf("bucket not found")
        }

        c := bucket.Cursor()
        for k, v := c.Seek(uint64ToBytes(start)); k != nil; k, v = c.Next() {
            var entry LogEntry
            currentIndex := bytesToUint64(k)
            
            if end > 0 && currentIndex > end {
                break
            }

            if err := json.Unmarshal(v, &entry); err != nil {
                return err
            }
            entries = append(entries, entry)
        }

        return nil
    })

    if err != nil {
        log.Fatalf("Error querying range: %v", err)
    }

    fmt.Printf("Found %d entries\n", len(entries))
    for _, entry := range entries {
        printEntry(entry)
    }
}

// Query latest N entries
func queryLatest(db *bolt.DB, n int) {
    var entries []LogEntry

    err := db.View(func(tx *bolt.Tx) error {
        bucket := tx.Bucket([]byte(BUCKET_NAME))
        if bucket == nil {
            return fmt.Errorf("bucket not found")
        }

        c := bucket.Cursor()
        for k, v := c.Last(); k != nil && len(entries) < n; k, v = c.Prev() {
            var entry LogEntry
            if err := json.Unmarshal(v, &entry); err != nil {
                return err
            }
            entries = append(entries, entry)
        }

        return nil
    })

    if err != nil {
        log.Fatalf("Error querying latest entries: %v", err)
    }

    fmt.Printf("Latest %d entries:\n", len(entries))
    for _, entry := range entries {
        printEntry(entry)
    }
}

// Get total count of entries
func getTotalCount(db *bolt.DB) {
    err := db.View(func(tx *bolt.Tx) error {
        bucket := tx.Bucket([]byte(BUCKET_NAME))
        if bucket == nil {
            return fmt.Errorf("bucket not found")
        }

        stats := bucket.Stats()
        fmt.Printf("Total entries: %d\n", stats.KeyN)
        return nil
    })

    if err != nil {
        log.Fatalf("Error getting count: %v", err)
    }
}

// Helper functions
func uint64ToBytes(n uint64) []byte {
    b := make([]byte, 8)
    binary.BigEndian.PutUint64(b, n)
    return b
}

func bytesToUint64(b []byte) uint64 {
    return binary.BigEndian.Uint64(b)
}

func printEntry(entry LogEntry) {
    fmt.Printf("\n=== Entry %d ===\n", entry.Index)
    fmt.Printf("Block Number: %d\n", entry.BlockNumber)
    fmt.Printf("Parent Hash: %s\n", entry.ParentHash)
    fmt.Printf("L1 Info Root: %s\n", entry.L1InfoRoot)
    fmt.Println("===============")
}
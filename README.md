# eth-log-indexer

A parallel event log indexer for Ethereum that processes and stores events in a BoltDB database with batched processing capabilities.

## Features

- Parallel processing of Ethereum event logs using worker batches
- Efficient storage using BoltDB
- Automatic batch generation based on block ranges
- Sequential indexing of events
- Concurrent log processing with goroutines
- Automatic database merging
- Error handling and logging
- Support for custom event topics and contract addresses

## Prerequisites

```bash
go >= 1.19
```

## Dependencies

```go
github.com/ethereum/go-ethereum
github.com/boltdb/bolt
```

## Installation

```bash
git clone https://github.com/yourusername/eth-log-indexer
cd eth-log-indexer
go mod download
```

## Configuration

Edit the constants in `main.go` to configure the indexer:

```go
CONTRACT_ADDR = "your_contract_address"
EVENT_TOPIC   = "your_event_topic"
RPC_ENDPOINT  = "your_ethereum_rpc_endpoint"
```

## Usage

```bash
go run main.go
```

The program will:
1. Create temporary worker databases for each batch
2. Process events in parallel across specified block ranges
3. Merge results into a final database (`final_logs.db`)
4. Clean up temporary files automatically

## Output Structure

Each log entry in the database contains:
```json
{
    "index": "sequential_index",
    "blockNumber": "block_number",
    "parentHash": "parent_block_hash",
    "l1InfoRoot": "l1_information_root"
}
```

## Performance Tuning

Adjust the number of batches in `main.go` to optimize for your system:

```go
batches, err := generateBatches(
    client,
    startBlock,  
    endBlock,  
    numberOfBatches,       
)
```

## Error Handling

The program includes comprehensive error handling:
- Worker errors are collected and reported
- Database operations are atomic
- Failed batches are logged for debugging

## Contributing

1. Fork the repository
2. Create your feature branch
3. Commit your changes
4. Push to the branch
5. Create a new Pull Request

## License

MIT

## Author

Aditya Roshan Joshi

## Acknowledgments

- Uses BoltDB for efficient key-value storage
- Built with go-ethereum client libraries

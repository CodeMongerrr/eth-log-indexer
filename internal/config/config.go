package config

import (
	"flag"
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the indexer service
type Config struct {
	// RPC
	RPC         string
	RPCTimeout  time.Duration
	RPCMaxRetry int

	// Contract
	ContractAddr string
	EventTopic   string

	// Storage
	DBPath      string
	StorageType string // "bolt" or "postgres"

	// Postgres (optional)
	PostgresURL string

	// Indexing
	Workers            int
	StartBlock         uint64
	EndBlock           uint64
	MaxBlockRange      uint64
	RollbackWindow     uint64
	Backfill           bool
	CheckpointInterval time.Duration

	// API
	APIPort        string
	APIAddr        string
	APIReadTimeout time.Duration

	// Metrics
	MetricsPort string
	MetricsAddr string

	// Logging
	LogLevel string
	LogJSON  bool

	// Shutdown
	ShutdownTimeout time.Duration
}

// LoadConfig loads configuration from flags and environment variables
func LoadConfig() *Config {
	cfg := &Config{}

	// RPC
	flag.StringVar(&cfg.RPC, "rpc", os.Getenv("RPC_URL"), "Ethereum RPC endpoint (env: RPC_URL)")
	flag.DurationVar(&cfg.RPCTimeout, "rpc-timeout", 30*time.Second, "RPC request timeout")
	flag.IntVar(&cfg.RPCMaxRetry, "rpc-max-retry", 3, "Max RPC retries with exponential backoff")

	// Contract
	flag.StringVar(&cfg.ContractAddr, "contract", os.Getenv("CONTRACT_ADDR"), "Contract address to index (env: CONTRACT_ADDR)")
	flag.StringVar(&cfg.EventTopic, "topic", os.Getenv("EVENT_TOPIC"), "Event topic hash to filter (env: EVENT_TOPIC)")

	// Storage
	flag.StringVar(&cfg.DBPath, "db", getEnvOrDefault("DB_PATH", "data/indexer.db"), "BoltDB path (env: DB_PATH)")
	flag.StringVar(&cfg.StorageType, "storage-type", "bolt", "Storage backend: bolt or postgres")
	flag.StringVar(&cfg.PostgresURL, "postgres-url", os.Getenv("POSTGRES_URL"), "Postgres connection URL (env: POSTGRES_URL)")

	// Indexing
	flag.IntVar(&cfg.Workers, "workers", getEnvOrDefaultInt("WORKERS", 8), "Parallel workers for backfill (env: WORKERS)")
	flag.Uint64Var(&cfg.StartBlock, "start", getEnvOrDefaultUint64("START_BLOCK", 0), "Start block for backfill (env: START_BLOCK)")
	flag.Uint64Var(&cfg.EndBlock, "end", getEnvOrDefaultUint64("END_BLOCK", 0), "End block for backfill (env: END_BLOCK)")
	flag.Uint64Var(&cfg.MaxBlockRange, "max-range", getEnvOrDefaultUint64("MAX_BLOCK_RANGE", 500), "Max blocks per RPC filter (env: MAX_BLOCK_RANGE)")
	flag.Uint64Var(&cfg.RollbackWindow, "rollback-window", getEnvOrDefaultUint64("ROLLBACK_WINDOW", 128), "Blocks to revalidate on reorg (env: ROLLBACK_WINDOW)")
	flag.BoolVar(&cfg.Backfill, "backfill", getEnvOrDefaultBool("BACKFILL", true), "Run historical backfill (env: BACKFILL)")
	flag.DurationVar(&cfg.CheckpointInterval, "checkpoint-interval", 30*time.Second, "Checkpoint persistence interval")

	// API
	flag.StringVar(&cfg.APIPort, "api-port", getEnvOrDefault("API_PORT", "8080"), "HTTP API port (env: API_PORT)")
	flag.StringVar(&cfg.APIAddr, "api-addr", getEnvOrDefault("API_ADDR", ":8080"), "HTTP API listen address (env: API_ADDR)")
	flag.DurationVar(&cfg.APIReadTimeout, "api-read-timeout", 10*time.Second, "API read timeout")

	// Metrics
	flag.StringVar(&cfg.MetricsPort, "metrics-port", getEnvOrDefault("METRICS_PORT", "9090"), "Prometheus metrics port (env: METRICS_PORT)")
	flag.StringVar(&cfg.MetricsAddr, "metrics-addr", getEnvOrDefault("METRICS_ADDR", ":9090"), "Prometheus listen address (env: METRICS_ADDR)")

	// Logging
	flag.StringVar(&cfg.LogLevel, "log-level", getEnvOrDefault("LOG_LEVEL", "info"), "Log level: debug, info, warn, error (env: LOG_LEVEL)")
	flag.BoolVar(&cfg.LogJSON, "log-json", getEnvOrDefaultBool("LOG_JSON", false), "Output logs as JSON (env: LOG_JSON)")

	// Shutdown
	flag.DurationVar(&cfg.ShutdownTimeout, "shutdown-timeout", 15*time.Second, "Graceful shutdown timeout")

	flag.Parse()

	return cfg
}

// Helper functions
func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvOrDefaultInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvOrDefaultUint64(key string, defaultVal uint64) uint64 {
	if val := os.Getenv(key); val != "" {
		if u, err := strconv.ParseUint(val, 10, 64); err == nil {
			return u
		}
	}
	return defaultVal
}

func getEnvOrDefaultBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
	}
	return defaultVal
}

// Validate checks that required config values are set
func (c *Config) Validate() error {
	if c.RPC == "" {
		return &ValidationError{Field: "rpc", Message: "RPC endpoint is required"}
	}
	if c.ContractAddr == "" {
		return &ValidationError{Field: "contract", Message: "contract address is required"}
	}
	if c.EventTopic == "" {
		return &ValidationError{Field: "topic", Message: "event topic is required"}
	}
	return nil
}

// ValidationError represents a configuration validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return "config: " + e.Field + ": " + e.Message
}

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/scylladb/sct-agent/internal/api"
	"github.com/scylladb/sct-agent/internal/executor"
	"github.com/scylladb/sct-agent/internal/storage"
)

type Config struct {
	Server struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
	} `yaml:"server"`

	Security struct {
		APIKeys []string `yaml:"api_keys"`
	} `yaml:"security"`

	Executor struct {
		MaxConcurrentJobs     int `yaml:"max_concurrent_jobs"`
		DefaultTimeoutSeconds int `yaml:"default_timeout_seconds"`
	} `yaml:"executor"`

	Logging struct {
		Level string `yaml:"level"`
	} `yaml:"logging"`

	Storage struct {
		Type                 string `yaml:"type"`
		CleanupIntervalHours int    `yaml:"cleanup_interval_hours"`
	} `yaml:"storage"`
}

func getDefaultConfig() *Config {
	return &Config{
		Server: struct {
			Host string `yaml:"host"`
			Port int    `yaml:"port"`
		}{
			Host: "0.0.0.0",
			Port: 16000,
		},

		Security: struct {
			APIKeys []string `yaml:"api_keys"`
		}{
			APIKeys: []string{"default-api-key"},
		},

		Executor: struct {
			MaxConcurrentJobs     int `yaml:"max_concurrent_jobs"`
			DefaultTimeoutSeconds int `yaml:"default_timeout_seconds"`
		}{
			MaxConcurrentJobs:     10,
			DefaultTimeoutSeconds: 1800,
		},

		Logging: struct {
			Level string `yaml:"level"`
		}{
			Level: "info",
		},

		Storage: struct {
			Type                 string `yaml:"type"`
			CleanupIntervalHours int    `yaml:"cleanup_interval_hours"`
		}{
			Type:                 "memory",
			CleanupIntervalHours: 24,
		},
	}
}

func loadConfig(configPath string) (*Config, error) {
	config := getDefaultConfig()
	if configPath == "" {
		log.Println("No config file specified, using defaults")
		return config, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("Config file %s not found, using defaults", configPath)
			return config, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err = yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// load API key from environment variable if set
	if apiKey := os.Getenv("SCT_AGENT_API_KEY"); apiKey != "" {
		log.Println("Loading API key from SCT_AGENT_API_KEY environment variable")
		config.Security.APIKeys = append(config.Security.APIKeys, apiKey)
	}

	if err = validateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return config, nil
}

func validateConfig(config *Config) error {
	if config.Server.Port <= 0 || config.Server.Port > 65535 {
		return fmt.Errorf("invalid port number: %d", config.Server.Port)
	}

	if len(config.Security.APIKeys) == 0 {
		return fmt.Errorf("at least one API key must be configured")
	}

	if config.Executor.MaxConcurrentJobs <= 0 {
		return fmt.Errorf("max_concurrent_jobs must be greater than 0")
	}

	if config.Executor.DefaultTimeoutSeconds <= 0 {
		return fmt.Errorf("default_timeout_seconds must be greater than 0")
	}

	return nil
}

func configureSlog(level string, logFilePath string) {
	logLevels := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
	}

	logLevel, exists := logLevels[level]
	if !exists {
		logLevel = slog.LevelInfo
	}

	var writer *os.File
	if logFilePath == "" {
		// no log file specified, use stdout
		writer = os.Stdout
	} else {
		// log to file to avoid overwhelming systemd-journald
		logFile, err := os.OpenFile(logFilePath,
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			// fallback to stdout if file cannot be opened
			log.Printf("Warning: failed to open log file %s, using stdout: %v", logFilePath, err)
			writer = os.Stdout
		} else {
			writer = logFile
		}
	}

	logger := slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)
}

const version = "0.0.1"

func main() {
	var configPath string
	var logFilePath string
	flag.StringVar(&configPath, "config", "configs/agent.yaml", "Path to configuration file")
	flag.StringVar(&logFilePath, "log-file", "", "Path to log file (empty = stdout)")
	flag.Parse()

	config, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	configureSlog(config.Logging.Level, logFilePath)

	slog.Info("Starting SCT Agent", "version", version)
	slog.Info("Server configuration", "host", config.Server.Host, "port", config.Server.Port)
	slog.Info("Executor configuration", "max_concurrent_jobs", config.Executor.MaxConcurrentJobs, "default_timeout_seconds", config.Executor.DefaultTimeoutSeconds)
	slog.Info("Logging configuration", "level", config.Logging.Level)

	var store storage.Storage
	switch config.Storage.Type {
	case "memory":
		store = storage.NewMemory()
		slog.Info("Storage initialized", "type", "memory")
	default:
		slog.Error("Unsupported storage type", "type", config.Storage.Type)
		os.Exit(1)
	}

	exec := executor.NewExecutor(config.Executor.MaxConcurrentJobs, store)

	httpServer := &http.Server{
		Addr:           fmt.Sprintf("%s:%d", config.Server.Host, config.Server.Port),
		Handler:        api.New(exec, config.Security.APIKeys, version).SetupRoutes(),
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1 MB
	}

	go func() {
		slog.Info("SCT Agent listening", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Failed to start server", "error", err)
			os.Exit(1)
		}
	}()

	// wait for interrupt for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Shutting down SCT Agent...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := exec.Shutdown(ctx); err != nil {
		slog.Error("Executor shutdown error", "error", err)
	}
	if err := httpServer.Shutdown(ctx); err != nil {
		slog.Error("HTTP server shutdown error", "error", err)
	}

	slog.Info("SCT Agent stopped")
}

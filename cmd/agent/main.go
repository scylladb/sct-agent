package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
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
			Port: 15000,
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

const version = "0.0.1"

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "configs/agent.yaml", "Path to configuration file")
	flag.Parse()

	config, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Starting SCT Agent v%s", version)
	log.Printf("Server will listen on %s:%d", config.Server.Host, config.Server.Port)
	log.Printf("Max concurrent jobs: %d", config.Executor.MaxConcurrentJobs)
	log.Printf("Default timeout: %d seconds", config.Executor.DefaultTimeoutSeconds)

	var store storage.Storage
	switch config.Storage.Type {
	case "memory":
		store = storage.NewMemory()
		log.Println("Using in-memory storage")
	default:
		log.Fatalf("Unsupported storage type: %s", config.Storage.Type)
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
		log.Printf("SCT Agent listening on %s", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// wait for interrupt for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down SCT Agent...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := exec.Shutdown(ctx); err != nil {
		log.Printf("Executor shutdown error: %v", err)
	}
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("SCT Agent stopped")
}

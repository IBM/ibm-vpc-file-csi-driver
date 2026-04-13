/**
 *
 * Copyright 2021- IBM Inc. All rights reserved
 * SPDX-License-Identifier: Apache2.0
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Package main - Tunnel Manager standalone executable
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/IBM/ibm-vpc-file-csi-driver/pkg/tunnel"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	tunnelManagerSocket = flag.String("socket", tunnel.DefaultSocketPath, "Unix domain socket path for tunnel-manager API")
	vendorVersion       string
	logger              *zap.Logger
)

func init() {
	logger = setUpLogger()
}

func main() {
	flag.Parse()
	defer func() {
		_ = logger.Sync() // #nosec G104: zap logger sync error is non-actionable
	}()

	if vendorVersion == "" {
		logger.Warn("Tunnel Manager version not set at compile time, using 'unknown'")
		vendorVersion = "unknown"
	}

	logger.Info("IBM VPC File CSI Driver - Tunnel Manager",
		zap.String("version", vendorVersion),
		zap.String("socketPath", *tunnelManagerSocket))

	// Get configuration from environment
	cfg := tunnel.GetConfigFromEnv(logger)

	// Create tunnel manager
	manager, err := tunnel.NewManager(cfg)
	if err != nil {
		logger.Fatal("Failed to initialize tunnel manager", zap.Error(err))
	}

	// Recover from any previous crash
	logger.Info("Starting crash recovery...")
	if err := manager.RecoverFromCrash(); err != nil {
		logger.Warn("Failed to recover tunnels, continuing anyway", zap.Error(err))
	} else {
		logger.Info("Crash recovery completed successfully")
	}

	// Create and start gRPC server
	server := tunnel.NewGRPCServer(manager, *tunnelManagerSocket, logger)
	if err := server.Start(); err != nil {
		logger.Fatal("Failed to start tunnel-manager gRPC server", zap.Error(err))
	}

	logger.Info("Tunnel Manager started successfully (gRPC)",
		zap.String("socketPath", *tunnelManagerSocket),
		zap.Int("basePort", cfg.BasePort),
		zap.Int("portRange", cfg.PortRange),
		zap.String("configDir", cfg.ConfigDir))

	// Wait for termination signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	sig := <-sigCh

	logger.Info("Received termination signal, shutting down gracefully",
		zap.String("signal", sig.String()))

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Stop(ctx); err != nil {
		logger.Error("Failed to stop tunnel-manager server cleanly", zap.Error(err))
		os.Exit(1)
	}

	if err := manager.Shutdown(); err != nil {
		logger.Error("Failed to shutdown tunnel manager cleanly", zap.Error(err))
		os.Exit(1)
	}

	logger.Info("Tunnel Manager shutdown completed successfully")
	os.Exit(0)
}

func setUpLogger() *zap.Logger {
	// Prepare a new logger
	atom := zap.NewAtomicLevel()
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "timestamp"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.Lock(os.Stdout),
		atom,
	), zap.AddCaller()).With(
		zap.String("component", "tunnel-manager"),
		zap.String("service", "ibm-vpc-file-csi-driver"))

	// Set log level from environment or default to Info
	logLevel := os.Getenv("LOG_LEVEL")
	switch logLevel {
	case "debug", "DEBUG":
		atom.SetLevel(zap.DebugLevel)
	case "warn", "WARN":
		atom.SetLevel(zap.WarnLevel)
	case "error", "ERROR":
		atom.SetLevel(zap.ErrorLevel)
	default:
		atom.SetLevel(zap.InfoLevel)
	}

	return logger
}

// Made with Bob

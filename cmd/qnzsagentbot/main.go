//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

// Command qnzsagentbot serves only POST /agentbots/{agent_id}/chat/completions.
// Canvas execution is the existing Go canvas runner (AgentService.buildRunFunc
// and canvas.Runner). This process does not register document or ingestion routes.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/qnzsagentbot"
	"ragflow/internal/server"
	"ragflow/internal/service"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer cancel()

	if err := common.InitLogger("info", common.FileOutput{}, "qnzsagentbot"); err != nil {
		log.Fatalf("logger: %v", err)
	}
	if err := server.InitLocalVariables(); err != nil {
		log.Fatalf("local variables: %v", err)
	}
	if err := server.Init(os.Getenv("RAGFLOW_CONFIG")); err != nil {
		log.Fatalf("config: %v", err)
	}
	if err := dao.InitDB(ctx, false); err != nil {
		log.Fatalf("mysql: %v", err)
	}

	store := qnzsagentbot.NewGormStore(dao.DB)
	svc := &qnzsagentbot.Service{
		Tokens:   store,
		Canvases: store,
		Runner:   &service.QNZSCanvasRunner{Agents: service.NewAgentService()},
	}
	addr := env("QNZS_LISTEN", ":9386")
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           svc.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// Canvas turns stream for longer than a single write deadline.
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	log.Printf("qnzs agentbot listening on %s", addr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

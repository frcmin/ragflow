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

// Command qnzsagentbot serves POST /agentbots/{agent_id}/chat/completions.
package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"ragflow/internal/qnzsagentbot"
)

func main() {
	dsn := mysqlDSN()
	db, err := qnzsagentbot.OpenMySQL(dsn)
	if err != nil {
		log.Fatalf("mysql: %v", err)
	}
	store := qnzsagentbot.NewGormStore(db)
	runner := &qnzsagentbot.HTTPCanvasRunner{
		URL:   env("QNZS_CANVAS_EXEC_URL", "http://127.0.0.1:9380/internal/qnzs/canvas/run"),
		Token: os.Getenv("QNZS_INTERNAL_TOKEN"),
		Client: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}
	if runner.Token == "" {
		log.Print("QNZS_INTERNAL_TOKEN is empty; canvas runs will fail until it matches the Python service")
	}
	svc := &qnzsagentbot.Service{
		Tokens:   store,
		Canvases: store,
		Runner:   runner,
	}
	addr := env("QNZS_LISTEN", ":9386")
	server := &http.Server{
		Addr:              addr,
		Handler:           svc.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("qnzs agentbot listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func mysqlDSN() string {
	user := env("MYSQL_USER", "root")
	password := os.Getenv("MYSQL_PASSWORD")
	host := env("MYSQL_HOST", "127.0.0.1")
	port := env("MYSQL_PORT", "3306")
	name := env("MYSQL_DBNAME", "rag_flow")
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=Local",
		user, password, host, port, url.PathEscape(name))
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

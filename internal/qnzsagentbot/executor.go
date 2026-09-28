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

package qnzsagentbot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RunRequest is one canvas execution after auth, ownership, and input mapping.
type RunRequest struct {
	TenantID  string
	AgentID   string
	SessionID string
	Query     string
	UserID    string
	Inputs    map[string]BeginInput
	Dialog    []DialogTurn
	HasDialog bool
}

// CanvasRunner executes the agent canvas. The production implementation calls
// the Python runtime; tests supply a fake.
type CanvasRunner interface {
	Run(ctx context.Context, req RunRequest) (<-chan string, error)
}

// HTTPCanvasRunner delegates canvas execution to the internal Python route.
type HTTPCanvasRunner struct {
	URL    string
	Token  string
	Client *http.Client
}

// Run posts the prepared request and streams message contents.
func (r *HTTPCanvasRunner) Run(ctx context.Context, req RunRequest) (<-chan string, error) {
	if r == nil || r.URL == "" || r.Token == "" {
		return nil, dataError("canvas executor is not configured")
	}
	body := map[string]any{
		"tenant_id":  req.TenantID,
		"agent_id":   req.AgentID,
		"session_id": req.SessionID,
		"query":      req.Query,
		"user_id":    req.UserID,
		"inputs":     req.Inputs,
	}
	if req.HasDialog {
		dialog := make([]map[string]string, 0, len(req.Dialog))
		for _, turn := range req.Dialog {
			dialog = append(dialog, map[string]string{"role": turn.Role, "content": turn.Content})
		}
		body["dialog"] = dialog
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, r.URL, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-QNZS-Internal-Token", r.Token)
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("canvas executor: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		message := strings.TrimSpace(string(payload))
		var wrapped struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(payload, &wrapped) == nil && wrapped.Message != "" {
			message = wrapped.Message
		}
		if message == "" {
			message = resp.Status
		}
		return nil, dataError(message)
	}
	out := make(chan string)
	go func() {
		defer resp.Body.Close()
		defer close(out)
		_ = readMessageContents(ctx, resp.Body, out)
	}()
	return out, nil
}

func readMessageContents(ctx context.Context, body io.Reader, out chan<- string) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var data strings.Builder
	flush := func() error {
		if data.Len() == 0 {
			return nil
		}
		payload := strings.TrimSpace(data.String())
		data.Reset()
		if payload == "" || payload == "[DONE]" {
			return nil
		}
		var event struct {
			Event string `json:"event"`
			Data  struct {
				Content any `json:"content"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return nil
		}
		if event.Event != "message" && event.Event != "error" {
			return nil
		}
		content, ok := event.Data.Content.(string)
		if !ok || content == "" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- content:
			return nil
		}
	}
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if rest, ok := strings.CutPrefix(line, "data:"); ok {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(rest))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}

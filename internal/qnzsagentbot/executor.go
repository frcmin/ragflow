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
	"context"
	"encoding/json"
)

// RunRequest is one canvas execution after auth and ownership checks.
// DSL, Inputs, and Dialog are what the existing Go canvas runner consumes.
// Raw keeps the caller JSON (with the suffixed session_id) for tests.
type RunRequest struct {
	TenantID  string
	AgentID   string
	SessionID string
	Query     string
	UserID    string
	DSL       map[string]any
	Inputs    map[string]BeginInput
	Dialog    []DialogTurn
	HasDialog bool
	Raw       map[string]any
}

// CanvasRunner executes the agent canvas. Production uses the Go canvas
// runner already owned by AgentService. Tests supply a fake.
type CanvasRunner interface {
	Run(ctx context.Context, req RunRequest) (<-chan string, error)
}

// ContentFromCanvasEvent returns assistant text from one canvas.RunEvent.
// qnzs_completion only forwards message content. Error text is included so a
// failed compile is not an empty stream. Workflow and node telemetry is not.
func ContentFromCanvasEvent(eventType, data string) (string, bool) {
	switch eventType {
	case "message":
		var event struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil || event.Content == "" {
			return "", false
		}
		return event.Content, true
	case "error":
		var event struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil || event.Message == "" {
			return "", false
		}
		return event.Message, true
	default:
		return "", false
	}
}

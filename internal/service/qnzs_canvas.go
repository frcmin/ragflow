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

package service

import (
	"context"
	"errors"

	"ragflow/internal/agent/canvas"
	dslpkg "ragflow/internal/agent/dsl"
	"ragflow/internal/qnzsagentbot"
)

// QNZSCanvasRunner runs a QNZS agentbot turn on the canvas runner AgentService
// already uses for /api/v1/agentbots/:id/completions.
type QNZSCanvasRunner struct {
	Agents *AgentService
}

// Run resets the saved canvas, applies dialog history and begin inputs, and
// streams assistant text from the existing canvas.Runner.
func (r *QNZSCanvasRunner) Run(ctx context.Context, req qnzsagentbot.RunRequest) (<-chan string, error) {
	if r == nil || r.Agents == nil {
		return nil, errors.New("canvas executor is not configured")
	}
	events, err := r.Agents.RunQNZSCanvas(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make(chan string)
	go func() {
		defer close(out)
		for event := range events {
			content, ok := qnzsagentbot.ContentFromCanvasEvent(event.Type, event.Data)
			if !ok {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case out <- content:
			}
		}
	}()
	return out, nil
}

// RunQNZSCanvas executes one qnzs_completion-shaped turn.
//
// It reuses buildRunFunc and canvas.Runner. The turn always starts from the
// user_canvas DSL: canvas.reset() semantics via ResetForCanvas, dialog
// replaces history, and canvas.run is not given a session id. agent_id is
// the task id. No API4Conversation row is created or resumed.
func (s *AgentService) RunQNZSCanvas(ctx context.Context, req qnzsagentbot.RunRequest) (<-chan canvas.RunEvent, error) {
	if s == nil || s.runner == nil {
		return nil, errors.New("canvas executor is not configured")
	}
	if req.AgentID == "" || len(req.DSL) == 0 {
		return nil, errors.New("Agent not found.")
	}
	dsl := prepareQNZSDSL(req)
	run := s.buildRunFunc(req.AgentID, nil, dsl)
	workflowInput := qnzsWorkflowInput(req)
	root := map[string]any{
		"canvas_id":  req.AgentID,
		"user_id":    req.UserID,
		"tenant_id":  req.TenantID,
		"user_input": workflowInput,
	}
	// Empty session id keeps runID equal to agent_id and skips conversation
	// persistence inside buildRunFunc (it returns when sessionID is blank).
	return s.runner.Run(ctx, run, req.AgentID, "", workflowInput, root), nil
}

func prepareQNZSDSL(req qnzsagentbot.RunRequest) map[string]any {
	dsl := dslpkg.ResetForCanvas(req.DSL)
	if req.HasDialog {
		history := make([]any, 0, len(req.Dialog))
		for _, turn := range req.Dialog {
			history = append(history, []any{turn.Role, turn.Content})
		}
		dsl["history"] = history
	}
	return dslpkg.NormalizeForRun(dsl)
}

func qnzsWorkflowInput(req qnzsagentbot.RunRequest) map[string]any {
	input := map[string]any{}
	if req.Query != "" {
		input["query"] = req.Query
	}
	if req.UserID != "" {
		input["user_id"] = req.UserID
	}
	for key, field := range req.Inputs {
		options := field.Options
		if options == nil {
			options = []any{}
		}
		input[key] = map[string]any{
			"name":     field.Name,
			"optional": field.Optional,
			"options":  options,
			"type":     field.Type,
			"value":    field.Value,
		}
	}
	return input
}

// Compile-time check that the adapter satisfies the endpoint runner.
var _ qnzsagentbot.CanvasRunner = (*QNZSCanvasRunner)(nil)

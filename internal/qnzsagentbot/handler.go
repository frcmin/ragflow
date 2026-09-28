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
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// CanvasRecord is the user_canvas row this endpoint checks.
type CanvasRecord struct {
	ID     string
	UserID string
	DSL    map[string]any
}

// CanvasStore loads an agent canvas by id.
type CanvasStore interface {
	GetByID(ctx context.Context, id string) (*CanvasRecord, error)
}

// ErrCanvasNotFound is returned when user_canvas has no such id.
var ErrCanvasNotFound = errors.New("canvas not found")

// Service is the QNZS agent-bot completion endpoint.
type Service struct {
	Tokens   TokenStore
	Canvases CanvasStore
	Runner   CanvasRunner
	Now      func() time.Time
}

// Handler returns the HTTP handler for POST /agentbots/{agent_id}/chat/completions.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /agentbots/{agent_id}/chat/completions", s.serve)
	mux.HandleFunc("POST /api/v1/agentbots/{agent_id}/chat/completions", s.serve)
	return mux
}

func (s *Service) serve(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agent_id")
	if agentID == "" {
		writeError(w, dataError("`agent_id` is required."))
		return
	}
	secret, apiErr := ParseAuthorization(r.Header.Get("Authorization"))
	if apiErr != nil {
		writeError(w, apiErr)
		return
	}
	tenantID, err := LookupTenant(r.Context(), s.Tokens, secret)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	body, err := readBody(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	sessionID, apiErr := ResolveSessionID(body, r.Header.Get("Session-Id"), agentID)
	if apiErr != nil {
		writeError(w, apiErr)
		return
	}
	canvas, err := s.Canvases.GetByID(r.Context(), agentID)
	if err != nil {
		if errors.Is(err, ErrCanvasNotFound) {
			writeError(w, dataError(msgAgentNotFound))
			return
		}
		writeAPIError(w, err)
		return
	}
	if canvas == nil || canvas.UserID != tenantID {
		if canvas == nil {
			writeError(w, dataError(msgAgentNotFound))
			return
		}
		writeError(w, dataError(msgAgentNotOwned))
		return
	}
	dialog, hasDialog, apiErr := DialogFromBody(body)
	if apiErr != nil {
		writeError(w, apiErr)
		return
	}
	if s.Runner == nil {
		writeError(w, dataError("canvas executor is not configured"))
		return
	}
	contents, err := s.Runner.Run(r.Context(), RunRequest{
		TenantID:  tenantID,
		AgentID:   agentID,
		SessionID: sessionID,
		Query:     RequestQuery(body),
		UserID:    RequestUserID(body),
		Inputs:    MapBeginInputs(BeginInputSchema(canvas.DSL), body),
		Dialog:    dialog,
		HasDialog: hasDialog,
	})
	if err != nil {
		writeAPIError(w, err)
		return
	}
	if !StreamRequested(body) {
		s.writeCollected(w, contents)
		return
	}
	s.writeStream(w, r, contents)
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) writeCollected(w http.ResponseWriter, contents <-chan string) {
	var text strings.Builder
	for content := range contents {
		text.WriteString(content)
	}
	completion, err := FormatCompletion(newCompletionID(), s.now().Unix(), text.String())
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": codeSuccess, "data": completion})
}

func (s *Service) writeStream(w http.ResponseWriter, r *http.Request, contents <-chan string) {
	SetSSEHeaders(w.Header())
	flusher, _ := w.(http.Flusher)
	for content := range contents {
		if content == "" {
			continue
		}
		if err := r.Context().Err(); err != nil {
			return
		}
		frame, err := FormatChunk(newCompletionID(), s.now().Unix(), content)
		if err != nil {
			return
		}
		if _, err := io.WriteString(w, frame); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func readBody(r *http.Request) (map[string]any, error) {
	if r.Body == nil || r.ContentLength == 0 {
		return map[string]any{}, nil
	}
	defer r.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		return nil, dataError("Invalid request body.")
	}
	if len(bytesTrim(raw)) == 0 {
		return map[string]any{}, nil
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil || body == nil {
		return nil, dataError("Invalid request body.")
	}
	return body, nil
}

func bytesTrim(raw []byte) []byte {
	return []byte(strings.TrimSpace(string(raw)))
}

func writeAPIError(w http.ResponseWriter, err error) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		writeError(w, apiErr)
		return
	}
	writeError(w, dataError(err.Error()))
}

func writeError(w http.ResponseWriter, err *APIError) {
	writeJSON(w, http.StatusOK, map[string]any{"code": err.Code, "message": err.Message})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	raw, err := marshalJSON(payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

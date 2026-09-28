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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeCanvases struct {
	row *CanvasRecord
}

func (f fakeCanvases) GetByID(_ context.Context, id string) (*CanvasRecord, error) {
	if f.row == nil || f.row.ID != id {
		return nil, ErrCanvasNotFound
	}
	return f.row, nil
}

type fakeRunner struct {
	req      RunRequest
	contents []string
}

func (f *fakeRunner) Run(_ context.Context, req RunRequest) (<-chan string, error) {
	f.req = req
	out := make(chan string, len(f.contents))
	for _, content := range f.contents {
		out <- content
	}
	close(out)
	return out, nil
}

func testService(tokens *fakeTokens, runner *fakeRunner) *Service {
	return &Service{
		Tokens: tokens,
		Canvases: fakeCanvases{row: &CanvasRecord{
			ID:     "agent-1",
			UserID: "tenant-1",
			DSL: map[string]any{
				"components": map[string]any{
					"begin": map[string]any{
						"obj": map[string]any{
							"component_name": "Begin",
							"params": map[string]any{
								"inputs": map[string]any{
									"customer.name": map[string]any{"type": "line", "optional": true},
									"dialog":        map[string]any{"type": "text"},
								},
							},
						},
					},
				},
			},
		}},
		Runner: runner,
		Now:    func() time.Time { return time.Unix(1_700_000_000, 0) },
	}
}

func TestHandlerStreamAndMapping(t *testing.T) {
	tokens := &fakeTokens{beta: map[string]string{"beta-key": "tenant-1"}}
	runner := &fakeRunner{contents: []string{"", "你", "好"}}
	srv := testService(tokens, runner)
	body := `{"query":"问题","customer":{"name":"张三"},"dialog":[{"role":"user","content":"旧问题"},{"role":"assistant","content":"旧回答"}]}`
	req := httptest.NewRequest(http.MethodPost, "/agentbots/agent-1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer beta-key")
	req.Header.Set("Session-Id", "from-header")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/event-stream; charset=utf-8" {
		t.Fatalf("content-type %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Cache-Control") != "no-cache" || rec.Header().Get("X-Accel-Buffering") != "no" || rec.Header().Get("Connection") != "keep-alive" {
		t.Fatalf("headers: %#v", rec.Header())
	}
	frames := strings.Split(rec.Body.String(), "\n\n")
	var contents []string
	for _, frame := range frames {
		frame = strings.TrimSpace(frame)
		if frame == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(frame, "data: ")), &payload); err != nil {
			t.Fatalf("frame %q: %v", frame, err)
		}
		if payload["object"] != "chat.completion.chunk" || payload["model"] != "agent-bot" {
			t.Fatalf("chunk: %#v", payload)
		}
		choice := payload["choices"].([]any)[0].(map[string]any)
		delta := choice["delta"].(map[string]any)
		contents = append(contents, delta["content"].(string))
	}
	if len(contents) != 2 || strings.Join(contents, "") != "你好" {
		t.Fatalf("empty chunk should be skipped, contents=%q", contents)
	}
	if runner.req.SessionID != "from-header-agent-1" || runner.req.TenantID != "tenant-1" || runner.req.Query != "问题" {
		t.Fatalf("run request: %+v", runner.req)
	}
	if runner.req.Raw["session_id"] != "from-header-agent-1" || runner.req.Raw["query"] != "问题" {
		t.Fatalf("raw kwargs: %#v", runner.req.Raw)
	}
	if _, ok := runner.req.Raw["inputs"]; ok {
		t.Fatal("raw kwargs must not gain a Go inputs map")
	}
	customer, _ := runner.req.Raw["customer"].(map[string]any)
	if customer["name"] != "张三" {
		t.Fatalf("raw customer: %#v", runner.req.Raw["customer"])
	}
	if runner.req.Inputs["customer.name"].Value != "张三" || runner.req.Inputs["dialog"].Value != "" {
		t.Fatalf("inputs: %+v", runner.req.Inputs)
	}
	if !runner.req.HasDialog || len(runner.req.Dialog) != 2 || runner.req.Dialog[1].Content != "旧回答" {
		t.Fatalf("dialog: %+v", runner.req.Dialog)
	}
}

func TestHandlerNonStream(t *testing.T) {
	tokens := &fakeTokens{token: map[string]string{"plain": "tenant-1"}}
	runner := &fakeRunner{contents: []string{"甲", "乙"}}
	srv := testService(tokens, runner)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agentbots/agent-1/chat/completions", strings.NewReader(`{"session_id":"s1","stream":false,"question":"q"}`))
	req.Header.Set("Authorization", "Bearer plain")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("content-type %q", rec.Header().Get("Content-Type"))
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["code"].(float64) != 0 {
		t.Fatalf("%s", rec.Body.String())
	}
	data := payload["data"].(map[string]any)
	if data["object"] != "chat.completion" {
		t.Fatalf("%#v", data)
	}
	choice := data["choices"].([]any)[0].(map[string]any)
	message := choice["message"].(map[string]any)
	if message["content"] != "甲乙" {
		t.Fatalf("content %q", message["content"])
	}
	if runner.req.SessionID != "s1-agent-1" || runner.req.Query != "q" {
		t.Fatalf("run: %+v", runner.req)
	}
	if runner.req.Raw["session_id"] != "s1-agent-1" || runner.req.Raw["question"] != "q" || runner.req.Raw["stream"] != false {
		t.Fatalf("raw: %#v", runner.req.Raw)
	}
}

func TestHandlerRejectsBadAuthAndForeignCanvas(t *testing.T) {
	tokens := &fakeTokens{beta: map[string]string{"beta-key": "other-tenant"}}
	srv := testService(tokens, &fakeRunner{})
	assertMessage := func(auth string, want string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/agentbots/agent-1/chat/completions", strings.NewReader(`{"session_id":"s"}`))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		var payload map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload["code"].(float64) != 102 || payload["message"] != want {
			t.Fatalf("got %#v want message %q", payload, want)
		}
	}
	assertMessage("", msgAuthInvalid)
	assertMessage("Bearer nope", msgAPIKeyInvalid)
	assertMessage("Bearer beta-key", msgAgentNotOwned)
}

func TestReadMessageContents(t *testing.T) {
	raw := "data: {\"event\":\"workflow_started\",\"data\":{}}\n\n" +
		"data: {\"event\":\"message\",\"data\":{\"content\":\"\"}}\n\n" +
		"data: {\"event\":\"message\",\"data\":{\"content\":\"你好\"}}\n\n"
	out := make(chan string, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := readMessageContents(context.Background(), strings.NewReader(raw), out); err != nil {
			t.Errorf("read: %v", err)
		}
		close(out)
	}()
	var got []string
	for content := range out {
		got = append(got, content)
	}
	<-done
	if strings.Join(got, "") != "你好" {
		t.Fatalf("got %#v", got)
	}
}

func TestReadOpenAIChunks(t *testing.T) {
	raw := "data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"\"}}]}\n\n" +
		"data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"故宫\"}}]}\n\n" +
		"data: {\"object\":\"chat.completion\",\"choices\":[{\"message\":{\"content\":\"ignored\"}}]}\n\n" +
		"data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"在北京\"}}]}\n\n"
	out := make(chan string, 4)
	if err := readMessageContents(context.Background(), strings.NewReader(raw), out); err != nil {
		t.Fatal(err)
	}
	close(out)
	var got []string
	for content := range out {
		got = append(got, content)
	}
	if strings.Join(got, "") != "故宫在北京" {
		t.Fatalf("got %#v", got)
	}
}

func TestHTTPCanvasRunnerForwardsRawKwargs(t *testing.T) {
	var got map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-QNZS-Internal-Token") != "secret" {
			t.Errorf("token header %q", r.Header.Get("X-QNZS-Internal-Token"))
		}
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"你好\"}}]}\n\n")
	}))
	defer upstream.Close()
	runner := &HTTPCanvasRunner{URL: upstream.URL, Token: "secret", Client: upstream.Client()}
	out, err := runner.Run(context.Background(), RunRequest{
		TenantID:  "tenant-1",
		AgentID:   "agent-1",
		SessionID: "s-agent-1",
		Query:     "not-sent-separately",
		Inputs:    map[string]BeginInput{"customer.name": {Name: "customer.name", Value: "should-not-send"}},
		Raw: map[string]any{
			"query":      "故宫在哪",
			"customer":   map[string]any{"name": "张三"},
			"session_id": "replaced",
			"dialog":     []any{map[string]any{"role": "user", "content": "hi"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var contents []string
	for content := range out {
		contents = append(contents, content)
	}
	if strings.Join(contents, "") != "你好" {
		t.Fatalf("contents %#v", contents)
	}
	if got["tenant_id"] != "tenant-1" || got["agent_id"] != "agent-1" || got["session_id"] != "s-agent-1" {
		t.Fatalf("ids %#v", got)
	}
	if got["query"] != "故宫在哪" {
		t.Fatalf("query %#v", got["query"])
	}
	if _, ok := got["inputs"]; ok {
		t.Fatalf("inputs must stay out of the qnzs_completion kwargs: %#v", got["inputs"])
	}
	customer, _ := got["customer"].(map[string]any)
	if customer["name"] != "张三" {
		t.Fatalf("customer %#v", got["customer"])
	}
	dialog, _ := got["dialog"].([]any)
	if len(dialog) != 1 {
		t.Fatalf("dialog %#v", got["dialog"])
	}
}

func TestHTTPCanvasRunnerStatusError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-QNZS-Internal-Token") != "secret" {
			t.Errorf("token header %q", r.Header.Get("X-QNZS-Internal-Token"))
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"code":102,"message":"Agent not found."}`)
	}))
	defer upstream.Close()
	runner := &HTTPCanvasRunner{URL: upstream.URL, Token: "secret", Client: upstream.Client()}
	_, err := runner.Run(context.Background(), RunRequest{AgentID: "a"})
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Message != "Agent not found." {
		t.Fatalf("err=%v", err)
	}
}

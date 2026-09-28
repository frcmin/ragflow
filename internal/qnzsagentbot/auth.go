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
	"strings"
)

const (
	codeSuccess   = 0
	codeDataError = 102

	// These strings match the custom Python endpoint, including the trailing quote.
	msgAuthInvalid   = `Authorization is not valid!"`
	msgAPIKeyInvalid = `Authentication error: API key is invalid!"`
	msgAgentNotFound = "Agent not found."
	msgAgentNotOwned = "You do not own the agent."

	modelName = "agent-bot"
)

// APIError is a business error returned as {"code","message"}.
type APIError struct {
	Code    int
	Message string
}

func (e *APIError) Error() string { return e.Message }

func dataError(message string) *APIError {
	return &APIError{Code: codeDataError, Message: message}
}

// TokenRecord is the api_token row the endpoint needs.
type TokenRecord struct {
	TenantID string
}

// TokenStore looks up API tokens. Beta is tried before the token column.
type TokenStore interface {
	FindByBeta(ctx context.Context, beta string) (*TokenRecord, error)
	FindByToken(ctx context.Context, token string) (*TokenRecord, error)
}

// ParseAuthorization splits "Bearer <secret>". The scheme word is not checked;
// any single space-separated pair is accepted, matching the Python split.
func ParseAuthorization(header string) (string, *APIError) {
	if strings.TrimSpace(header) == "" {
		return "", dataError(msgAuthInvalid)
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || parts[1] == "" {
		return "", dataError(msgAuthInvalid)
	}
	return parts[1], nil
}

// LookupTenant resolves tenant_id from the API token table: beta, then token.
func LookupTenant(ctx context.Context, store TokenStore, secret string) (string, error) {
	rec, err := store.FindByBeta(ctx, secret)
	if err != nil {
		return "", err
	}
	if rec == nil {
		rec, err = store.FindByToken(ctx, secret)
		if err != nil {
			return "", err
		}
	}
	if rec == nil || rec.TenantID == "" {
		return "", dataError(msgAPIKeyInvalid)
	}
	return rec.TenantID, nil
}

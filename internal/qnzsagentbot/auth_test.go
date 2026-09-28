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
	"testing"
)

type fakeTokens struct {
	beta  map[string]string
	token map[string]string
	order []string
}

func (f *fakeTokens) FindByBeta(_ context.Context, beta string) (*TokenRecord, error) {
	f.order = append(f.order, "beta")
	tenant, ok := f.beta[beta]
	if !ok {
		return nil, nil
	}
	return &TokenRecord{TenantID: tenant}, nil
}

func (f *fakeTokens) FindByToken(_ context.Context, token string) (*TokenRecord, error) {
	f.order = append(f.order, "token")
	tenant, ok := f.token[token]
	if !ok {
		return nil, nil
	}
	return &TokenRecord{TenantID: tenant}, nil
}

func TestParseAuthorization(t *testing.T) {
	secret, err := ParseAuthorization("Bearer secret-1")
	if err != nil || secret != "secret-1" {
		t.Fatalf("secret=%q err=%v", secret, err)
	}
	if _, err := ParseAuthorization(""); err == nil || err.Message != msgAuthInvalid {
		t.Fatalf("missing header: %v", err)
	}
	if _, err := ParseAuthorization("Bearer"); err == nil || err.Message != msgAuthInvalid {
		t.Fatalf("single part: %v", err)
	}
	secret, err = ParseAuthorization("Token abc def")
	if err != nil || secret != "abc def" {
		t.Fatalf("split once: secret=%q err=%v", secret, err)
	}
}

func TestLookupTenantBetaThenToken(t *testing.T) {
	store := &fakeTokens{
		beta:  map[string]string{"beta-key": "tenant-beta"},
		token: map[string]string{"plain-key": "tenant-token", "beta-key": "should-not-win"},
	}
	tenant, err := LookupTenant(context.Background(), store, "beta-key")
	if err != nil || tenant != "tenant-beta" {
		t.Fatalf("beta tenant=%q err=%v", tenant, err)
	}
	if len(store.order) != 1 || store.order[0] != "beta" {
		t.Fatalf("beta lookup should stop before token column: %v", store.order)
	}

	store.order = nil
	tenant, err = LookupTenant(context.Background(), store, "plain-key")
	if err != nil || tenant != "tenant-token" {
		t.Fatalf("token tenant=%q err=%v", tenant, err)
	}
	if len(store.order) != 2 || store.order[0] != "beta" || store.order[1] != "token" {
		t.Fatalf("order=%v", store.order)
	}

	store.order = nil
	_, err = LookupTenant(context.Background(), store, "missing")
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Message != msgAPIKeyInvalid {
		t.Fatalf("invalid key: %v", err)
	}
}

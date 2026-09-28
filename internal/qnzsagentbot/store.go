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
	"database/sql/driver"
	"encoding/json"
	"errors"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// OpenMySQL opens the shared RAGFlow metadata database.
func OpenMySQL(dsn string) (*gorm.DB, error) {
	return gorm.Open(mysql.Open(dsn), &gorm.Config{})
}

// GormStore reads api_token and user_canvas. QNZSUserCanvasService is this
// user_canvas table: there is no separate QNZS table in this fork.
type GormStore struct {
	db *gorm.DB
}

// NewGormStore wraps a database handle.
func NewGormStore(db *gorm.DB) *GormStore {
	return &GormStore{db: db}
}

type apiTokenRow struct {
	TenantID string  `gorm:"column:tenant_id"`
	Beta     *string `gorm:"column:beta"`
	Token    string  `gorm:"column:token"`
}

func (apiTokenRow) TableName() string { return "api_token" }

type canvasRow struct {
	ID     string `gorm:"column:id"`
	UserID string `gorm:"column:user_id"`
	DSL    dslMap `gorm:"column:dsl"`
}

func (canvasRow) TableName() string { return "user_canvas" }

type dslMap map[string]any

func (m *dslMap) Scan(value any) error {
	if value == nil {
		*m = nil
		return nil
	}
	var raw []byte
	switch typed := value.(type) {
	case []byte:
		raw = typed
	case string:
		raw = []byte(typed)
	default:
		return errors.New("user_canvas.dsl is not text")
	}
	if len(raw) == 0 {
		*m = map[string]any{}
		return nil
	}
	return json.Unmarshal(raw, m)
}

func (m dslMap) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

// FindByBeta returns the first api_token row whose beta column matches.
func (s *GormStore) FindByBeta(ctx context.Context, beta string) (*TokenRecord, error) {
	var rows []apiTokenRow
	err := s.db.WithContext(ctx).Where("beta = ?", beta).Limit(1).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &TokenRecord{TenantID: rows[0].TenantID}, nil
}

// FindByToken returns the api_token row whose token column matches.
func (s *GormStore) FindByToken(ctx context.Context, token string) (*TokenRecord, error) {
	var rows []apiTokenRow
	err := s.db.WithContext(ctx).Where("token = ?", token).Limit(1).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &TokenRecord{TenantID: rows[0].TenantID}, nil
}

// GetByID loads one user_canvas row. A missing row is ErrCanvasNotFound.
func (s *GormStore) GetByID(ctx context.Context, id string) (*CanvasRecord, error) {
	var row canvasRow
	err := s.db.WithContext(ctx).Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCanvasNotFound
	}
	if err != nil {
		return nil, err
	}
	dsl := map[string]any(row.DSL)
	if dsl == nil {
		dsl = map[string]any{}
	}
	return &CanvasRecord{ID: row.ID, UserID: row.UserID, DSL: dsl}, nil
}

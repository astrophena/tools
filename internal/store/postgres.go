// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// PostgresStore is a PostgreSQL implementation of the Store interface.
type PostgresStore struct {
	pool *pgxpool.Pool
	ttl  time.Duration
}

// NewPostgresStore creates a new PostgresStore and connects to the database.
func NewPostgresStore(ctx context.Context, databaseURL string, ttl time.Duration) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}

	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS kv (
			key TEXT PRIMARY KEY,
			value JSONB NOT NULL,
			last_accessed TIMESTAMPTZ NOT NULL
		);
	`); err != nil {
		return nil, err
	}

	s := &PostgresStore{
		pool: pool,
		ttl:  ttl,
	}
	go s.cleanup(ctx)
	return s, nil
}

func (s *PostgresStore) cleanup(ctx context.Context) {
	ticker := time.NewTicker(s.ttl)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.pool.Exec(ctx, `DELETE FROM kv WHERE last_accessed < NOW() - $1;`, s.ttl.String())
		case <-ctx.Done():
			return
		}
	}
}

// Get retrieves a value for a given key.
func (s *PostgresStore) Get(ctx context.Context, key string) (starlark.Value, error) {
	var data []byte
	if err := s.pool.QueryRow(ctx, `
		UPDATE kv SET last_accessed = NOW() WHERE key = $1
		RETURNING value;
	`, key).Scan(&data); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return starlark.None, nil
		}
		return nil, err
	}
	return jsonToStarlark(data)
}

// Set stores a value for a given key.
func (s *PostgresStore) Set(ctx context.Context, key string, value starlark.Value) error {
	data, err := starlarkToJSON(value)
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO kv (key, value, last_accessed)
		VALUES ($1, $2, NOW())
		ON CONFLICT (key) DO UPDATE
		SET value = $2, last_accessed = NOW();
	`, key, data)
	return err
}

// Close closes the database connection.
func (s *PostgresStore) Close() error {
	s.pool.Close()
	return nil
}

func starlarkToJSON(v starlark.Value) ([]byte, error) {
	switch v := v.(type) {
	case starlark.NoneType:
		return json.Marshal(nil)
	case starlark.Bool:
		return json.Marshal(bool(v))
	case starlark.Int:
		i, ok := v.Int64()
		if !ok {
			return nil, fmt.Errorf("int too large: %s", v.String())
		}
		return json.Marshal(i)
	case starlark.Float:
		return json.Marshal(float64(v))
	case starlark.String:
		return json.Marshal(v.GoString())
	case *starlark.List:
		var list []any
		for i := 0; i < v.Len(); i++ {
			item, err := starlarkToJSON(v.Index(i))
			if err != nil {
				return nil, err
			}
			list = append(list, json.RawMessage(item))
		}
		return json.Marshal(list)
	case *starlark.Dict:
		dict := make(map[string]any)
		for _, item := range v.Items() {
			k, v := item[0], item[1]
			key, ok := k.(starlark.String)
			if !ok {
				return nil, fmt.Errorf("dict key is not a string: %s", k.Type())
			}
			val, err := starlarkToJSON(v)
			if err != nil {
				return nil, err
			}
			dict[key.GoString()] = json.RawMessage(val)
		}
		return json.Marshal(dict)
	case starlark.Tuple:
		var list []any
		for i := 0; i < v.Len(); i++ {
			item, err := starlarkToJSON(v.Index(i))
			if err != nil {
				return nil, err
			}
			list = append(list, json.RawMessage(item))
		}
		return json.Marshal(map[string]any{
			"__starlark_type__": "tuple",
			"values":          list,
		})
	case *starlarkstruct.Struct:
		dict := make(map[string]any)
		for _, name := range v.AttrNames() {
			val, err := v.Attr(name)
			if err != nil {
				return nil, err
			}
			item, err := starlarkToJSON(val)
			if err != nil {
				return nil, err
			}
			dict[name] = json.RawMessage(item)
		}
		return json.Marshal(map[string]any{
			"__starlark_type__": "struct",
			"values":          dict,
		})
	default:
		return nil, fmt.Errorf("unsupported type: %s", v.Type())
	}
}

func jsonToStarlark(data []byte) (starlark.Value, error) {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return jsonToStarlarkValue(v)
}

func jsonToStarlarkValue(v any) (starlark.Value, error) {
	switch v := v.(type) {
	case nil:
		return starlark.None, nil
	case bool:
		return starlark.Bool(v), nil
	case float64:
		if float64(int64(v)) == v {
			return starlark.MakeInt64(int64(v)), nil
		}
		return starlark.Float(v), nil
	case string:
		return starlark.String(v), nil
	case []any:
		var list []starlark.Value
		for _, item := range v {
			val, err := jsonToStarlarkValue(item)
			if err != nil {
				return nil, err
			}
			list = append(list, val)
		}
		return starlark.NewList(list), nil
	case map[string]any:
		if t, ok := v["__starlark_type__"].(string); ok {
			switch t {
			case "tuple":
				values, ok := v["values"].([]any)
				if !ok {
					return nil, fmt.Errorf("invalid tuple format")
				}
				var list []starlark.Value
				for _, item := range values {
					val, err := jsonToStarlarkValue(item)
					if err != nil {
						return nil, err
					}
					list = append(list, val)
				}
				return starlark.Tuple(list), nil
			case "struct":
				values, ok := v["values"].(map[string]any)
				if !ok {
					return nil, fmt.Errorf("invalid struct format")
				}
				var tuples []starlark.Tuple
				for k, item := range values {
					val, err := jsonToStarlarkValue(item)
					if err != nil {
						return nil, err
					}
					tuples = append(tuples, starlark.Tuple{starlark.String(k), val})
				}
				return starlarkstruct.FromKeywords(starlark.String("struct"), tuples), nil
			}
		}
		dict := starlark.NewDict(0)
		for k, item := range v {
			val, err := jsonToStarlarkValue(item)
			if err != nil {
				return nil, err
			}
			if err := dict.SetKey(starlark.String(k), val); err != nil {
				return nil, err
			}
		}
		return dict, nil
	default:
		return nil, fmt.Errorf("unsupported type: %T", v)
	}
}

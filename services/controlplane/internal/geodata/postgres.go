package geodata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// PostgresStore uses the Zone Studio API view and save function. The compatible
// schema is applied by the signed appliance chart before this service is ready.
type PostgresStore struct{ DB *sql.DB }

func (s PostgresStore) Ready(ctx context.Context) error {
	if s.DB == nil {
		return errors.New("geo database is required")
	}
	var version string
	if err := s.DB.QueryRowContext(ctx, `SELECT version FROM public.zone_schema_migrations WHERE version = '004_migration_history.sql'`).Scan(&version); err != nil {
		return fmt.Errorf("geo schema readiness: %w", err)
	}
	return nil
}

func (s PostgresStore) List(ctx context.Context) (json.RawMessage, error) {
	if s.DB == nil {
		return nil, errors.New("geo database is required")
	}
	var result []byte
	err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(json_agg(row_to_json(z) ORDER BY z.name, z.id), '[]'::json) FROM api.zones z`).Scan(&result)
	if err != nil {
		return nil, fmt.Errorf("list zones: %w", err)
	}
	return json.RawMessage(result), nil
}

func (s PostgresStore) Export(ctx context.Context) (json.RawMessage, error) {
	if s.DB == nil {
		return nil, errors.New("geo database is required")
	}
	var result []byte
	if err := s.DB.QueryRowContext(ctx, `SELECT api.export_zones()`).Scan(&result); err != nil {
		return nil, fmt.Errorf("export zones: %w", err)
	}
	return json.RawMessage(result), nil
}

func (s PostgresStore) Save(ctx context.Context, request SaveRequest) (string, error) {
	if s.DB == nil {
		return "", errors.New("geo database is required")
	}
	var id string
	err := s.DB.QueryRowContext(ctx, `SELECT api.save_zone($1, $2, $3::jsonb, $4::uuid, $5::integer)::text`, request.Name, request.Description, string(request.Geometry), request.ID, request.Revision).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("save zone: %w", err)
	}
	return id, nil
}

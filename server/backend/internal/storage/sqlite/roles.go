package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"appliance-code/server/backend/internal/storage"
)

// RoleStore is the SQLite-backed storage.RoleStore.
type RoleStore struct {
	db *DB
}

// NewRoleStore returns a RoleStore backed by db.
func NewRoleStore(db *DB) *RoleStore {
	return &RoleStore{db: db}
}

func (s *RoleStore) UpsertPermission(ctx context.Context, p storage.Permission) error {
	_, err := s.db.q(ctx).ExecContext(ctx, `
		INSERT INTO permissions (name, description) VALUES (?, ?)
		ON CONFLICT (name) DO UPDATE SET description = excluded.description`,
		p.Name, p.Description,
	)
	if err != nil {
		return fmt.Errorf("sqlite: upserting permission %s: %w", p.Name, err)
	}
	return nil
}

func (s *RoleStore) ListPermissions(ctx context.Context) ([]storage.Permission, error) {
	rows, err := s.db.q(ctx).QueryContext(ctx, `SELECT name, description FROM permissions ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing permissions: %w", err)
	}
	defer rows.Close()

	var perms []storage.Permission
	for rows.Next() {
		var p storage.Permission
		if err := rows.Scan(&p.Name, &p.Description); err != nil {
			return nil, err
		}
		perms = append(perms, p)
	}
	return perms, rows.Err()
}

func (s *RoleStore) UpsertRole(ctx context.Context, r storage.Role) error {
	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = now
	}
	_, err := s.db.q(ctx).ExecContext(ctx, `
		INSERT INTO roles (id, name, built_in, created_at, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			name = excluded.name,
			built_in = excluded.built_in,
			updated_at = excluded.updated_at`,
		r.ID, r.Name, boolToInt(r.BuiltIn), r.CreatedAt.Format(time.RFC3339Nano), r.UpdatedAt.Format(time.RFC3339Nano),
	)
	if isUniqueConstraintErr(err) {
		return storage.ErrConflict
	}
	if err != nil {
		return fmt.Errorf("sqlite: upserting role %s: %w", r.Name, err)
	}
	return nil
}

func scanRole(row interface{ Scan(dest ...any) error }) (storage.Role, error) {
	var (
		r                    storage.Role
		builtIn              int
		createdAt, updatedAt string
	)
	if err := row.Scan(&r.ID, &r.Name, &builtIn, &createdAt, &updatedAt); err != nil {
		return storage.Role{}, err
	}
	r.BuiltIn = builtIn != 0
	var err error
	if r.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return storage.Role{}, err
	}
	if r.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return storage.Role{}, err
	}
	return r, nil
}

const selectRoleColumns = `id, name, built_in, created_at, updated_at`

func (s *RoleStore) Get(ctx context.Context, id string) (storage.Role, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `SELECT `+selectRoleColumns+` FROM roles WHERE id = ?`, id)
	r, err := scanRole(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.Role{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.Role{}, fmt.Errorf("sqlite: getting role %s: %w", id, err)
	}
	return r, nil
}

func (s *RoleStore) GetByName(ctx context.Context, name string) (storage.Role, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `SELECT `+selectRoleColumns+` FROM roles WHERE name = ?`, name)
	r, err := scanRole(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.Role{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.Role{}, fmt.Errorf("sqlite: getting role by name %s: %w", name, err)
	}
	return r, nil
}

func (s *RoleStore) List(ctx context.Context) ([]storage.Role, error) {
	rows, err := s.db.q(ctx).QueryContext(ctx, `SELECT `+selectRoleColumns+` FROM roles ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing roles: %w", err)
	}
	defer rows.Close()

	var roles []storage.Role
	for rows.Next() {
		r, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		roles = append(roles, r)
	}
	return roles, rows.Err()
}

func (s *RoleStore) Delete(ctx context.Context, id string) error {
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		if _, err := s.db.q(ctx).ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id = ?`, id); err != nil {
			return fmt.Errorf("sqlite: deleting role permissions for %s: %w", id, err)
		}
		if _, err := s.db.q(ctx).ExecContext(ctx, `DELETE FROM user_roles WHERE role_id = ?`, id); err != nil {
			return fmt.Errorf("sqlite: deleting user role assignments for %s: %w", id, err)
		}
		res, err := s.db.q(ctx).ExecContext(ctx, `DELETE FROM roles WHERE id = ?`, id)
		if err != nil {
			return fmt.Errorf("sqlite: deleting role %s: %w", id, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return storage.ErrNotFound
		}
		return nil
	})
}

func (s *RoleStore) SetRolePermissions(ctx context.Context, roleID string, permissionNames []string) error {
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		if _, err := s.db.q(ctx).ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id = ?`, roleID); err != nil {
			return fmt.Errorf("sqlite: clearing permissions for role %s: %w", roleID, err)
		}
		for _, name := range permissionNames {
			if _, err := s.db.q(ctx).ExecContext(ctx,
				`INSERT INTO role_permissions (role_id, permission_name) VALUES (?, ?)`, roleID, name,
			); err != nil {
				return fmt.Errorf("sqlite: granting permission %s to role %s: %w", name, roleID, err)
			}
		}
		return nil
	})
}

func (s *RoleStore) ListRolePermissions(ctx context.Context, roleID string) ([]string, error) {
	rows, err := s.db.q(ctx).QueryContext(ctx,
		`SELECT permission_name FROM role_permissions WHERE role_id = ? ORDER BY permission_name ASC`, roleID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing permissions for role %s: %w", roleID, err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func (s *RoleStore) AssignUserRole(ctx context.Context, userID, roleID string) error {
	_, err := s.db.q(ctx).ExecContext(ctx, `
		INSERT INTO user_roles (user_id, role_id, created_at) VALUES (?, ?, ?)
		ON CONFLICT (user_id, role_id) DO NOTHING`,
		userID, roleID, time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("sqlite: assigning role %s to user %s: %w", roleID, userID, err)
	}
	return nil
}

func (s *RoleStore) RemoveUserRole(ctx context.Context, userID, roleID string) error {
	_, err := s.db.q(ctx).ExecContext(ctx, `DELETE FROM user_roles WHERE user_id = ? AND role_id = ?`, userID, roleID)
	if err != nil {
		return fmt.Errorf("sqlite: removing role %s from user %s: %w", roleID, userID, err)
	}
	return nil
}

func (s *RoleStore) SetUserRoles(ctx context.Context, userID string, roleIDs []string) error {
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		if _, err := s.db.q(ctx).ExecContext(ctx, `DELETE FROM user_roles WHERE user_id = ?`, userID); err != nil {
			return fmt.Errorf("sqlite: clearing roles for user %s: %w", userID, err)
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		for _, roleID := range roleIDs {
			if _, err := s.db.q(ctx).ExecContext(ctx,
				`INSERT INTO user_roles (user_id, role_id, created_at) VALUES (?, ?, ?)`, userID, roleID, now,
			); err != nil {
				return fmt.Errorf("sqlite: assigning role %s to user %s: %w", roleID, userID, err)
			}
		}
		return nil
	})
}

func (s *RoleStore) ListUserRoles(ctx context.Context, userID string) ([]storage.Role, error) {
	rows, err := s.db.q(ctx).QueryContext(ctx, `
		SELECT r.id, r.name, r.built_in, r.created_at, r.updated_at
		FROM roles r
		JOIN user_roles ur ON ur.role_id = r.id
		WHERE ur.user_id = ?
		ORDER BY r.name ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing roles for user %s: %w", userID, err)
	}
	defer rows.Close()

	var roles []storage.Role
	for rows.Next() {
		r, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		roles = append(roles, r)
	}
	return roles, rows.Err()
}

func (s *RoleStore) ListUsersWithRole(ctx context.Context, roleID string) ([]string, error) {
	rows, err := s.db.q(ctx).QueryContext(ctx, `SELECT user_id FROM user_roles WHERE role_id = ?`, roleID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing users with role %s: %w", roleID, err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

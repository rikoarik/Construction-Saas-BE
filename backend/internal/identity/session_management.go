package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// SessionInfo deliberately excludes bearer credentials and their hashes.
type SessionInfo struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Current   bool      `json:"current"`
}

type AccountInfo struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type CompanyAccess struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

func (s *Store) Account(ctx context.Context, userID string) (AccountInfo, error) {
	var a AccountInfo
	err := s.pool.QueryRow(ctx, `SELECT id::text, email FROM app.users WHERE id=$1::uuid AND status='active'`, userID).Scan(&a.ID, &a.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, fmt.Errorf("load account: %w", err)
	}
	return a, nil
}

func (s *Store) Sessions(ctx context.Context, userID, currentID string) ([]SessionInfo, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text, created_at, expires_at FROM app.auth_sessions WHERE user_id=$1::uuid AND revoked_at IS NULL AND expires_at>now() ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	result := make([]SessionInfo, 0)
	for rows.Next() {
		var item SessionInfo
		if err := rows.Scan(&item.ID, &item.CreatedAt, &item.ExpiresAt); err != nil {
			return nil, err
		}
		item.Current = item.ID == currentID
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// Companies uses only active memberships and tenants; no platform grant is implied.
func (s *Store) Companies(ctx context.Context, userID string) ([]CompanyAccess, error) {
	rows, err := s.pool.Query(ctx, `SELECT c.id::text,c.name,m.role FROM app.company_memberships m JOIN app.companies c ON c.id=m.company_id WHERE m.user_id=$1::uuid AND m.status='active' AND c.status='active' ORDER BY c.name,c.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list companies: %w", err)
	}
	defer rows.Close()
	result := make([]CompanyAccess, 0)
	for rows.Next() {
		var item CompanyAccess
		if err := rows.Scan(&item.ID, &item.Name, &item.Role); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

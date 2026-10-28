package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

var ErrCompanyAccess = errors.New("company access denied")

// SelectCompany records a convenience context, not an authorization grant.
// Every company operation must independently recheck current membership and status.
func (s *Store) SelectCompany(ctx context.Context, session Session, companyID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Lock the session and current membership and company through commit, so a
	// concurrent suspension or revocation cannot commit during this selection.
	var allowed bool
	err = tx.QueryRow(ctx, `SELECT true FROM app.company_memberships m
 JOIN app.companies c ON c.id=m.company_id
 JOIN app.users u ON u.id=m.user_id
 JOIN app.auth_sessions s ON s.user_id=u.id
 WHERE s.id=$1::uuid AND s.token_hash=$2 AND s.revoked_at IS NULL
 AND s.auth_state='active' AND s.expires_at>now()
 AND u.status='active' AND u.security_version=s.security_version
 AND m.company_id=$3::uuid AND m.status='active' AND c.status='active'
 FOR UPDATE OF s, m, c`, session.ID, session.TokenHash, companyID).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCompanyAccess
	}
	if err != nil {
		return fmt.Errorf("check company selection: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE app.auth_sessions SET selected_company_id=$2::uuid WHERE id=$1::uuid`, session.ID, companyID); err != nil {
		return fmt.Errorf("select company: %w", err)
	}
	return tx.Commit(ctx)
}

// SelectedCompany revalidates current access, including account and session status.
func (s *Store) SelectedCompany(ctx context.Context, session Session) (*CompanyAccess, error) {
	var c CompanyAccess
	err := s.pool.QueryRow(ctx, `SELECT c.id::text,c.name,m.role FROM app.auth_sessions s
 JOIN app.users u ON u.id=s.user_id
 JOIN app.companies c ON c.id=s.selected_company_id
 JOIN app.company_memberships m ON m.company_id=c.id AND m.user_id=u.id
 WHERE s.id=$1::uuid AND s.token_hash=$2 AND s.revoked_at IS NULL
 AND s.auth_state='active' AND s.expires_at>now()
 AND u.status='active' AND u.security_version=s.security_version
 AND c.status='active' AND m.status='active'`, session.ID, session.TokenHash).Scan(&c.ID, &c.Name, &c.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load selected company: %w", err)
	}
	return &c, nil
}

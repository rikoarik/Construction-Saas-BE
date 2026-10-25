package projects

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrDenied  = errors.New("project permission denied")
	ErrInvalid = errors.New("invalid project permission request")
)

type GrantPolicy map[string]map[string]struct{}

// Authorizer applies explicit role-to-permission grants and default-deny
// membership checks. Company roles do not inherit project permissions.
type Authorizer struct{ pool *pgxpool.Pool }

func NewAuthorizer(pool *pgxpool.Pool) (*Authorizer, error) {
	if pool == nil {
		return nil, errors.New("PostgreSQL pool is required")
	}
	return &Authorizer{pool: pool}, nil
}

func (a *Authorizer) Check(ctx context.Context, userID, companyID, projectID, permission string, policy GrantPolicy) error {
	if a == nil || a.pool == nil || userID == "" || companyID == "" || projectID == "" || permission == "" {
		return ErrInvalid
	}
	var role, companyStatus, projectStatus, membershipStatus string
	err := a.pool.QueryRow(ctx, `
		SELECT pm.role, cm.status, p.status, pm.status
		FROM app.project_memberships pm
		JOIN app.company_memberships cm ON cm.company_id=pm.company_id AND cm.user_id=pm.user_id
		JOIN app.projects p ON p.company_id=pm.company_id AND p.id=pm.project_id
		WHERE pm.company_id=$1::uuid AND pm.project_id=$2::uuid AND pm.user_id=$3::uuid`, companyID, projectID, userID).Scan(&role, &companyStatus, &projectStatus, &membershipStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDenied
	}
	if err != nil {
		return fmt.Errorf("resolve project permission context: %w", err)
	}
	if companyStatus != "active" || projectStatus != "active" || membershipStatus != "active" {
		return ErrDenied
	}
	if policy == nil {
		return ErrDenied
	}
	permissions, ok := policy[role]
	if !ok {
		return ErrDenied
	}
	if _, ok := permissions[permission]; !ok {
		return ErrDenied
	}
	return nil
}

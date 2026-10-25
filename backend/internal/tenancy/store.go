package tenancy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction-saas/backend/internal/audit"
)

var (
	ErrInvalid   = errors.New("invalid company operation")
	ErrNotFound  = errors.New("company resource not found")
	ErrForbidden = errors.New("company operation forbidden")
	ErrConflict  = errors.New("company operation conflict")
)

type Company struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Status    string    `json:"status"`
	Role      string    `json:"role,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
type Member struct {
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}
type Invitation struct {
	ID        string    `json:"id"`
	CompanyID string    `json:"company_id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
	Token     string    `json:"token,omitempty"`
}
type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, errors.New("PostgreSQL pool is required")
	}
	return &Store{pool: pool}, nil
}
func normalizeSlug(v string) string  { return strings.ToLower(strings.TrimSpace(v)) }
func normalizeEmail(v string) string { return strings.ToLower(strings.TrimSpace(v)) }

func (s *Store) CreateCompany(ctx context.Context, ownerID, name, slug string) (Company, error) {
	name, slug = strings.TrimSpace(name), normalizeSlug(slug)
	if ownerID == "" || name == "" || slug == "" || len(name) > 200 || len(slug) > 120 || strings.ContainsAny(slug, " /\\") {
		return Company{}, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Company{}, fmt.Errorf("begin company creation: %w", err)
	}
	defer tx.Rollback(ctx)
	var c Company
	err = tx.QueryRow(ctx, `INSERT INTO app.companies(name,slug) VALUES($1,$2) RETURNING id::text,name,slug,status,created_at`, name, slug).Scan(&c.ID, &c.Name, &c.Slug, &c.Status, &c.CreatedAt)
	if err != nil {
		if pgErrCode(err) == "23505" {
			return Company{}, ErrConflict
		}
		return Company{}, fmt.Errorf("create company: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO app.company_memberships(company_id,user_id,role,status) VALUES($1::uuid,$2::uuid,'owner','active')`, c.ID, ownerID); err != nil {
		return Company{}, fmt.Errorf("create owner membership: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO app.company_settings(company_id) VALUES($1::uuid)`, c.ID); err != nil {
		return Company{}, fmt.Errorf("create settings: %w", err)
	}
	companyID, actorID := c.ID, ownerID
	metadata, _ := json.Marshal(map[string]string{"slug": slug})
	if err := audit.Append(ctx, tx, audit.Event{CompanyID: &companyID, ActorID: &actorID, Action: "company.created", EntityType: "company", Metadata: metadata}); err != nil {
		return Company{}, fmt.Errorf("audit company creation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Company{}, fmt.Errorf("commit company creation: %w", err)
	}
	c.Role = "owner"
	return c, nil
}

func (s *Store) Membership(ctx context.Context, companyID, userID string) (string, error) {
	var role string
	err := s.pool.QueryRow(ctx, `SELECT role FROM app.company_memberships WHERE company_id=$1::uuid AND user_id=$2::uuid AND status='active'`, companyID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("company membership: %w", err)
	}
	return role, nil
}

func (s *Store) ListCompanies(ctx context.Context, userID string) ([]Company, error) {
	rows, err := s.pool.Query(ctx, `SELECT c.id::text,c.name,c.slug,c.status,m.role,c.created_at FROM app.companies c JOIN app.company_memberships m ON m.company_id=c.id WHERE m.user_id=$1::uuid AND m.status='active' ORDER BY c.name,c.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list companies: %w", err)
	}
	defer rows.Close()
	items := make([]Company, 0)
	for rows.Next() {
		var c Company
		if err := rows.Scan(&c.ID, &c.Name, &c.Slug, &c.Status, &c.Role, &c.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) ListMembers(ctx context.Context, companyID, userID string, limit int) ([]Member, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	role, err := s.Membership(ctx, companyID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	if role != "owner" && role != "admin" {
		return nil, ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT m.user_id::text,u.email,m.role,m.status,m.created_at FROM app.company_memberships m JOIN app.users u ON u.id=m.user_id WHERE m.company_id=$1::uuid AND m.status<>'removed' ORDER BY m.created_at,m.user_id LIMIT $2`, companyID, limit)
	if err != nil {
		return nil, fmt.Errorf("list company members: %w", err)
	}
	defer rows.Close()
	items := make([]Member, 0)
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Email, &m.Role, &m.Status, &m.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) CreateInvitation(ctx context.Context, actorID, companyID, email, role string, ttl time.Duration) (Invitation, error) {
	email = strings.TrimSpace(email)
	normalized, role := normalizeEmail(email), strings.ToLower(strings.TrimSpace(role))
	if actorID == "" || companyID == "" || normalized == "" || len(normalized) > 320 || ttl <= 0 || ttl > 30*24*time.Hour || (role != "admin" && role != "member" && role != "viewer") {
		return Invitation{}, ErrInvalid
	}
	actorRole, err := s.Membership(ctx, companyID, actorID)
	if err != nil {
		return Invitation{}, ErrNotFound
	}
	if actorRole != "owner" && actorRole != "admin" {
		return Invitation{}, ErrForbidden
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return Invitation{}, fmt.Errorf("generate invitation token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	digest := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(digest[:])
	expires := time.Now().Add(ttl)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Invitation{}, err
	}
	defer tx.Rollback(ctx)
	var item Invitation
	err = tx.QueryRow(ctx, `INSERT INTO app.company_invitations(company_id,email,email_normalized,token_hash,role,expires_at,created_by) VALUES($1::uuid,$2,$3,$4,$5,$6,$7::uuid) RETURNING id::text,company_id::text,email,role,expires_at`, companyID, email, normalized, tokenHash, role, expires, actorID).Scan(&item.ID, &item.CompanyID, &item.Email, &item.Role, &item.ExpiresAt)
	if err != nil {
		return Invitation{}, fmt.Errorf("create invitation: %w", err)
	}
	company, actor, entity := companyID, actorID, item.ID
	metadata, _ := json.Marshal(map[string]string{"role": role})
	if err := audit.Append(ctx, tx, audit.Event{CompanyID: &company, ActorID: &actor, Action: "company.invitation.created", EntityType: "company_invitation", EntityID: &entity, Metadata: metadata}); err != nil {
		return Invitation{}, fmt.Errorf("audit invitation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Invitation{}, fmt.Errorf("commit invitation: %w", err)
	}
	item.Token = token
	return item, nil
}

func (s *Store) ListInvitations(ctx context.Context, actorID, companyID string, limit int) ([]Invitation, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	role, err := s.Membership(ctx, companyID, actorID)
	if err != nil {
		return nil, ErrNotFound
	}
	if role != "owner" && role != "admin" {
		return nil, ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT id::text,company_id::text,email,role,expires_at FROM app.company_invitations WHERE company_id=$1::uuid ORDER BY created_at DESC,id DESC LIMIT $2`, companyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Invitation, 0)
	for rows.Next() {
		var item Invitation
		if err := rows.Scan(&item.ID, &item.CompanyID, &item.Email, &item.Role, &item.ExpiresAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func HashInvitationToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
func pgErrCode(err error) string {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState()
	}
	return ""
}

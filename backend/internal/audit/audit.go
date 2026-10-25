package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

var ErrInvalid = errors.New("invalid audit event")

// Event describes an audit record. Company and project identity must be
// resolved from trusted backend state, never accepted as authorization proof
// from a client payload. Metadata is limited contextual data, not a request or
// response body; callers must redact secrets and unnecessary personal data.
type Event struct {
	CompanyID  *string
	ProjectID  *string
	ActorID    *string
	Action     string
	EntityType string
	EntityID   *string
	RequestID  string
	Metadata   json.RawMessage
}

// Append inserts an event using the caller's transaction so audit history and
// the business mutation can commit or roll back atomically.
func Append(ctx context.Context, tx pgx.Tx, event Event) error {
	if tx == nil {
		return fmt.Errorf("%w: transaction is required", ErrInvalid)
	}
	if event.CompanyID == nil || strings.TrimSpace(*event.CompanyID) == "" || strings.TrimSpace(event.Action) != event.Action || event.Action == "" || len(event.Action) > 128 || strings.TrimSpace(event.EntityType) != event.EntityType || event.EntityType == "" || len(event.EntityType) > 128 {
		return fmt.Errorf("%w: company, action, and entity type are required", ErrInvalid)
	}
	if len(event.RequestID) > 128 {
		return fmt.Errorf("%w: request ID exceeds 128 characters", ErrInvalid)
	}
	metadata := event.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(metadata, &object); err != nil || object == nil {
		return fmt.Errorf("%w: metadata must be a JSON object", ErrInvalid)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO app.audit_logs
			(company_id, project_id, actor_id, action, entity_type, entity_id, request_id, metadata_json)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6::uuid, NULLIF($7, ''), $8::jsonb)`,
		event.CompanyID, event.ProjectID, event.ActorID, event.Action, event.EntityType, event.EntityID, event.RequestID, metadata)
	if err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}
	return nil
}

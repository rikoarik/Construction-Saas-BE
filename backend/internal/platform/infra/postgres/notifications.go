package postgres

import (
	"context"
	"fmt"

	"construction-saas/backend/internal/platform/app"
)

// notificationSource derives the operator's header feed from records the platform
// already keeps. Nothing is copied into a notification table: a job that recovers
// simply stops producing an entry, so the feed cannot drift from the facts.
//
// Only rows that describe a current problem are included. Delivered events and
// first-attempt pending jobs are not notifications; a retry-pending event is,
// because it has already failed at least once. Legacy dead letters have no
// recorded failure time, so they fall back to their creation time: leaving them
// out would hide a failure the jobs table still lists.
const notificationSource = `SELECT
 'process:' || e.id::text AS key,
 CASE WHEN e.status = 'dead' THEN 'process_failed' ELSE 'process_retry' END AS type,
 CASE WHEN e.status = 'dead' THEN 'error' ELSE 'warning' END AS severity,
 e.company_id::text AS company_id,
 c.name AS company_name,
 e.event_type,
 e.error_code,
 NULL::text AS plan,
 coalesce(e.last_failed_at, e.created_at) AS occurred_at
 FROM app.outbox_events e
 LEFT JOIN app.companies c ON c.id = e.company_id
 WHERE e.status = 'dead' OR (e.status = 'pending' AND e.last_failed_at IS NOT NULL)
UNION ALL
SELECT 'company:' || c.id::text, 'company_suspended', 'warning', c.id::text, c.name, NULL, NULL, NULL, c.updated_at
 FROM app.companies c WHERE c.status = 'suspended'
UNION ALL
SELECT 'subscription:' || s.id::text, 'subscription_past_due', 'warning', s.company_id::text, c.name, NULL, NULL, p.name, s.updated_at
 FROM app.subscriptions s
 JOIN app.companies c ON c.id = s.company_id
 JOIN app.plans p ON p.id = s.plan_id
 WHERE s.status = 'past_due'`

// Notifications returns the newest entries first, with read state resolved for one
// operator. The unread total is counted before filtering so the badge does not
// change when the operator narrows the list.
func (r DashboardRead) Notifications(ctx context.Context, operator string, types []string, read string, limit int) (app.NotificationPage, error) {
	page := app.NotificationPage{Items: make([]app.Notification, 0)}
	if err := r.Pool.QueryRow(ctx, `WITH derived AS (`+notificationSource+`)
 SELECT count(*) FROM derived d
 WHERE NOT EXISTS (
   SELECT 1 FROM app.platform_notification_reads r
   WHERE r.user_id = $1 AND r.notification_key = d.key
 )`, operator).Scan(&page.Unread); err != nil {
		return page, fmt.Errorf("notification unread count: %w", err)
	}
	// A nil type slice means "every type"; it is bound as SQL NULL and guarded so
	// the comparison never evaluates to NULL.
	rows, err := r.Pool.Query(ctx, `WITH derived AS (`+notificationSource+`)
 SELECT d.key, d.type, d.severity, d.company_id, d.company_name, d.event_type, d.error_code, d.plan, d.occurred_at,
        r.read_at IS NOT NULL AS read
 FROM derived d
 LEFT JOIN app.platform_notification_reads r
   ON r.user_id = $1 AND r.notification_key = d.key
 WHERE ($2::text[] IS NULL OR d.type = ANY($2))
   AND ($3 = '' OR ($3 = 'unread' AND r.read_at IS NULL) OR ($3 = 'read' AND r.read_at IS NOT NULL))
 ORDER BY d.occurred_at DESC, d.key DESC
 LIMIT $4`, operator, types, read, limit)
	if err != nil {
		return page, fmt.Errorf("notifications: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item app.Notification
		if err := rows.Scan(&item.Key, &item.Type, &item.Severity, &item.CompanyID, &item.CompanyName,
			&item.EventType, &item.ErrorCode, &item.Plan, &item.OccurredAt, &item.Read); err != nil {
			return page, err
		}
		page.Items = append(page.Items, item)
	}
	return page, rows.Err()
}

// MarkNotifications writes or removes read markers. Marking an entry read twice is
// idempotent; clearing a marker that does not exist is a no-op.
func (r DashboardRead) MarkNotifications(ctx context.Context, operator string, keys []string, read bool) error {
	if read {
		if _, err := r.Pool.Exec(ctx, `INSERT INTO app.platform_notification_reads (user_id, notification_key)
 SELECT $1, unnest($2::text[])
 ON CONFLICT (user_id, notification_key) DO NOTHING`, operator, keys); err != nil {
			return fmt.Errorf("mark notifications read: %w", err)
		}
		return nil
	}
	if _, err := r.Pool.Exec(ctx, `DELETE FROM app.platform_notification_reads
 WHERE user_id = $1 AND notification_key = ANY($2::text[])`, operator, keys); err != nil {
		return fmt.Errorf("mark notifications unread: %w", err)
	}
	return nil
}

// MarkAllNotificationsRead only marks entries that currently exist, so the table
// does not accumulate markers for notifications the operator never saw.
func (r DashboardRead) MarkAllNotificationsRead(ctx context.Context, operator string) error {
	if _, err := r.Pool.Exec(ctx, `WITH derived AS (`+notificationSource+`)
 INSERT INTO app.platform_notification_reads (user_id, notification_key)
 SELECT $1, d.key FROM derived d
 ON CONFLICT (user_id, notification_key) DO NOTHING`, operator); err != nil {
		return fmt.Errorf("mark all notifications read: %w", err)
	}
	return nil
}

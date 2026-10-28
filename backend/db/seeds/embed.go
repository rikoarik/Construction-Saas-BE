package seeds

import _ "embed"

// DashboardDemo is the insert-only demo dataset for the platform dashboard.
//
//go:embed dashboard_demo.sql
var DashboardDemo string

// DemoAccountDomain marks every seeded user. It is a reserved TLD, so the
// accounts can never receive mail or be mistaken for real people.
const DemoAccountDomain = "@seed.invalid"

// DemoRequestID marks the audit rows written by the dataset. audit_logs is
// append-only, so this marker is how a re-run avoids duplicating them.
const DemoRequestID = "seed:dashboard-demo"

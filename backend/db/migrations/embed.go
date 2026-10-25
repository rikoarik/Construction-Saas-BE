package migrations

import "embed"

// FS contains the versioned PostgreSQL schema migrations.
//
//go:embed *.sql
var FS embed.FS

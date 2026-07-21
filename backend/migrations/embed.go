// Package migrations embeds forward SQL migrations into the migration executable.
package migrations

import "embed"

// Files contains all approved forward migrations.
//
//go:embed *.up.sql
var Files embed.FS

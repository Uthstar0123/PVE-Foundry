package migrations

import "embed"

// Files contains versioned, transactional PostgreSQL migrations.
//
//go:embed *.up.sql
var Files embed.FS

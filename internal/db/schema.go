package db

import "embed"

//go:embed migrations/*.sql
var migrationFS embed.FS

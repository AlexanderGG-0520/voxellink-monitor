package migrations

import "embed"

// Files is the immutable, versioned schema source embedded in every service image.
//
//go:embed *.sql
var Files embed.FS

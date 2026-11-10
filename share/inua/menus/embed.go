// Package menufiles holds the menu packs shipped with the shell.
// A pack is one TOML file; see docs/appmenus.md.
package menufiles

import "embed"

// Files is the packs compiled into the shell. A directory of the same
// name on disk overrides a pack with the same id.
//
//go:embed *.toml
var Files embed.FS

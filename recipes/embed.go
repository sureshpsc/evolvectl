// Package recipes embeds the built-in migration recipes.
package recipes

import "embed"

// FS contains recipe.yaml files and their golden fixtures.
//
//go:embed go python
var FS embed.FS

// Package muda exposes the bundled delivery-waste reference content. Recipes
// are not bundled: they come in packs (internal/packs).
package muda

import "embed"

// Content contains the standard and the skills.
//
//go:embed standard skills
var Content embed.FS

// Package muda exposes the bundled delivery-waste reference content. Recipes
// are not bundled: they come in packs (internal/packs).
package muda

import (
	"embed"
	"io/fs"
)

// Content contains the standard and the skills.
//
//go:embed standard skills
var Content embed.FS

//go:embed skills/*/SKILL.md
var stockSkills embed.FS

// BuiltinSkills exposes only stock SKILL.md files, without reference standards,
// the skills README, or any synced pack content.
func BuiltinSkills() fs.FS {
	scoped, _ := fs.Sub(stockSkills, "skills") // The embedded directory always exists.
	return scoped
}

package record

import (
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/subaud-tools/muda/internal/content"
	"github.com/subaud-tools/muda/internal/packs"
	"github.com/subaud-tools/muda/internal/version"
)

// candidatePrefix marks a waste from the standard's candidate list.
const candidatePrefix = "candidate:"

// shipped is muda's standard with the packs in muda's cache.
var shipped = sync.OnceValues(func() (content.Vocabulary, error) {
	lib, err := packs.Default().Library(version.Version().Number)
	return lib.Vocabulary(), err
})

func (s *Store) vocabulary(file string) (content.Vocabulary, error) {
	if s.Vocab != nil {
		return *s.Vocab, nil
	}
	v, err := shipped()
	if err != nil {
		return v, problem("invalid-vocabulary", file, err.Error())
	}
	return v, nil
}

var (
	// packClass is a class ID outside muda's own W catalog: a pack's.
	packClass = regexp.MustCompile(`^[A-VX-Z][1-9][0-9]*$`)
	recipeID  = regexp.MustCompile(`^R[1-9][0-9]*$`)
)

// unchecked reports whether f names a pack class or a recipe ID that v
// doesn't have: one in a pack this muda doesn't hold, which it can't check.
func unchecked(v content.Vocabulary, f Finding) bool {
	return (packClass.MatchString(f.Waste) && !slices.Contains(v.Wastes, f.Waste)) ||
		(recipeID.MatchString(f.Recipe) && !slices.Contains(v.Recipes, f.Recipe))
}

// checkWaste accepts a catalog W-class or candidate:<slug> for a listed
// candidate, and reports which it is.
func checkWaste(v content.Vocabulary, file, id, waste string) (candidate bool, err error) {
	if slug, ok := strings.CutPrefix(waste, candidatePrefix); ok {
		if slices.Contains(v.Candidates, slug) {
			return true, nil
		}
	} else if slices.Contains(v.Wastes, waste) {
		return false, nil
	}
	return false, problem("unknown-waste", file, id+": "+waste)
}

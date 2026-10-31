package serve_test

import (
	"testing"

	"github.com/subaud-tools/tools-common/pageserve/conformance"
)

// muda's serve follows the family's server rules, tested from outside on the
// built binary.
func TestServerConforms(t *testing.T) {
	conformance.Check(t, conformance.Server{
		Tool: "muda", Binary: conformance.Build(t, "../../cmd/muda"),
		Args: []string{"serve", "--foreground", "--no-open"}, Channel: true,
	})
}

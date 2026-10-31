package serve

import (
	"flag"
	"testing"

	"github.com/subaud-tools/muda/internal/store"
	"github.com/subaud-tools/tools-common/wiregen"
)

// updateWire rewrites web/wire.d.ts instead of asserting it.
var updateWire = flag.Bool("update", false, "rewrite web/wire.d.ts from the Go structs")

// wireRoots is the contract: what the page reads and writes. Everything they
// reach follows.
var wireRoots = []any{
	ProjectList{},
	ProjectView{},
	store.Project{},
	DraftBody{},
	ProjectBody{},
	SendResult{},
	ScanResult{},
	AgentStatus{},
}

const wireHeader = `// wire.d.ts: the Go and page contract, generated. Do not edit.
//
// Written by internal/serve/wire_test.go from the json tags of the API's types
// and every type they reach. Regenerate with ` + "`go test ./internal/serve -run WireType -update`" + `.
`

func TestTheWireTypeIsCommitted(t *testing.T) {
	opts := wiregen.Options{Header: wireHeader, Regenerate: "go test ./internal/serve -run WireType -update"}
	if err := wiregen.CheckCommitted("../web/wire.d.ts", wireRoots, opts, *updateWire); err != nil {
		t.Fatal(err)
	}
}

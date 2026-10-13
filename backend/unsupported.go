package backend

import (
	"fmt"
	"sort"

	"github.com/zobstory/cakebear/ir"
)

// Unsupported is one piece of IR this backend cannot emit yet.
type Unsupported struct {
	Span ir.Span
	What string
}

// UnsupportedError reports IR that lower/ accepted but this backend cannot
// emit yet. It is a limit of the current phase, never a fault in the program,
// so the CLI reports it the way it reports lowering's refusals rather than as
// a failed go build.
type UnsupportedError struct {
	Nodes []Unsupported
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("the backend cannot emit %d construct(s) yet", len(e.Nodes))
}

// describeUnsupported names a node the way its author would.
func describeUnsupported(node any) string {
	switch n := node.(type) {
	case *ir.HostCall:
		return "the backend cannot emit node:http's " + n.Op.String() + " yet"
	case *ir.HostProp:
		return "the backend cannot emit node:http's " + n.Prop.String() + " yet"
	default:
		return fmt.Sprintf("the backend cannot emit %T yet", node)
	}
}

// sortedUnsupported orders findings by source position. Emission visits
// functions before main, which is not the order a reader goes through a file.
func sortedUnsupported(u []Unsupported) []Unsupported {
	sort.SliceStable(u, func(i, j int) bool {
		if u[i].Span.Line != u[j].Span.Line {
			return u[i].Span.Line < u[j].Span.Line
		}
		return u[i].Span.Col < u[j].Span.Col
	})
	return u
}

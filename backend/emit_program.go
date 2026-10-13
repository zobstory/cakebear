package backend

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/zobstory/cakebear/ir"
)

// A program of several modules (M3 plan D5) becomes one Go package.
//
//   - A module's top-level names, its functions and top-level variables, are
//     prefixed ts_m<i>_ with its index in ir.Program.Modules, so two modules'
//     `helper`s stay apart. ts_ is the namespace mangle keeps for itself: the
//     only mangled names starting ts_m are ts_map, ts_make, ts_max, ts_min and
//     ts_main, none followed by a digit, so no local can collide with one.
//   - Each module but the entry has its top-level code in ts_m<i>_init, and
//     main calls those in ir.Program.EvaluationOrder, then runs the entry's
//     own top-level code.
//
// A program of one module is emitted as it always was, without prefixes.

// program renders p's Go: the top-level variables, the functions, the other
// modules' init functions, then main.
func (e *emitter) program(p *ir.Program) string {
	if len(p.Modules) > 1 {
		e.prefixes = make(map[string]string, len(p.Modules))
		for i, m := range p.Modules {
			e.prefixes[m.File] = fmt.Sprintf("ts_m%d_", i)
		}
	}

	// Functions and main are rendered into separate buffers before anything is
	// assembled, because whether the runtime is used at all is only known once
	// every statement has been walked -- a program whose only output comes from
	// inside a called function still needs the buffer flushed, and a program
	// that never logs must not import the runtime, since Go rejects an unused
	// import.
	//
	// Top-level variables are declared at package level, so a top-level
	// function can use them, and assigned where the source initialises them, so
	// side effects keep their order. A package-level initialiser would run
	// before main, ahead of any top-level code written earlier in the file.
	e.hoisted = map[*ir.VarDecl]bool{}
	var globals, funcs, inits strings.Builder
	for i, m := range p.Modules {
		e.cur = m.File
		globals.WriteString(e.capture(func() {
			for _, s := range m.Main {
				if d, ok := s.(*ir.VarDecl); ok {
					fmt.Fprintf(&e.buf, "var %s %s\n", e.top(m.File, d.Name), e.typ(d.Type))
					e.hoisted[d] = true
				}
			}
		}))
		funcs.WriteString(e.capture(func() {
			for _, fn := range m.Funcs {
				e.function(fn)
				e.buf.WriteString("\n")
			}
		}))
		if i != p.Entry {
			inits.WriteString(e.capture(func() { e.initFunc(i, m) }))
		}
	}

	entry := p.EntryModule()
	e.cur = entry.File
	e.usesDomain = false
	mainBody := e.capture(func() {
		for _, i := range p.EvaluationOrder() {
			if i != p.Entry {
				fmt.Fprintf(&e.buf, "\t%s(%s)\n", initName(i), e.dom())
			}
		}
		if entry.Async {
			// A top-level await: main's code is an async body, and its
			// promise tells RunModule whether the module ever finished.
			e.asyncBodyAs(moduleVar+" := ", ir.PromiseOf(ir.Void), entry.Main, 1)
		} else {
			e.stmts(entry.Main, 1)
		}
	})
	mainDomain := e.usesDomain
	if mainDomain {
		e.usesRuntime = true
	}

	var out strings.Builder
	if globals.Len() > 0 {
		out.WriteString(globals.String() + "\n")
	}
	out.WriteString(funcs.String())
	out.WriteString(inits.String())
	out.WriteString("func main() {\n")
	if e.usesRuntime {
		out.WriteString("\tdefer " + rtPkg + ".Flush()\n")
	}
	if mainDomain {
		// Top-level code runs in the main program's domain (domain.go).
		out.WriteString("\t" + domainVar + " := " + rtPkg + ".Main()\n")
	}
	out.WriteString(mainBody)
	if entry.Async {
		// Run, then exit 13 if the top-level await never settled, as Node does.
		fmt.Fprintf(&out, "\t%s.RunModule(%s, %s)\n", rtPkg, moduleVar, strconv.Quote(entry.File))
	} else if e.needsRun {
		// Last, so listen callbacks run after the top-level code, as in Node.
		out.WriteString("\t" + rtPkg + ".Run()\n")
	}
	out.WriteString("}\n")
	return out.String()
}

// top names a binding for Go: name, prefixed with its module's prefix when
// module, the file declaring it at its top level, is one of several, and as
// it is for a function's own binding.
func (e *emitter) top(module, name string) string {
	if prefix, ok := e.prefixes[module]; ok && module != "" {
		return prefix + mangle(name)
	}
	return mangle(name)
}

// initName is the function holding module i's top-level code. mangle renames
// any name `init`, so no prefixed name can be this.
func initName(i int) string { return fmt.Sprintf("ts_m%d_init", i) }

// initFunc renders module i's top-level code as the function main calls to
// evaluate it, in the main program's domain.
func (e *emitter) initFunc(i int, m *ir.Module) {
	e.usesRuntime = true
	fmt.Fprintf(&e.buf, "func %s(%s) {\n", initName(i), domainParam)
	e.stmts(m.Main, 1)
	e.buf.WriteString("}\n\n")
}

package ir

import (
	"errors"
	"fmt"
)

// Program is a whole program: every module it compiles, and the one the
// binary runs (M3 plan D5).
//
// A reference to a name declared at a module's top level says which module
// (Ident.Module, Call.Module, Assign.Module, Capture.Module), because two
// modules may each declare a top-level `helper`. A name local to a function
// says nothing: Go's own scoping resolves it.
type Program struct {
	// Modules is every module, sorted by File. The order is deterministic, so
	// anything a backend derives from it, such as a per-module prefix on
	// top-level names, is the same however the frontend ran.
	Modules []*Module
	// Entry is the index in Modules of the module the binary runs. A native
	// binary has one entry point, where tsc needs none.
	Entry int
	// Shapes is every object shape the program's types use, each at its own
	// ID: Shapes.All of the interner lowering used, shared by all modules.
	Shapes []*Shape
}

// ProgramOf is the program of one module, which is its entry.
func ProgramOf(m *Module) *Program { return &Program{Modules: []*Module{m}} }

// EntryModule is the module the binary runs.
func (p *Program) EntryModule() *Module { return p.Modules[p.Entry] }

// EvaluationOrder is the order the modules' top-level code runs in, by index
// in Modules, as an ES module graph evaluates: depth first from the entry,
// each module after the modules it imports, in the order it imports them. A
// module already on the way is not entered again, which is what settles a
// cycle, and the entry comes last. A module the entry never reaches, through
// imports that evaluate it, never runs. (Measured on Node 24.18: a graph of
// main → a → b → a, main → c → b prints b, a, c, main.)
func (p *Program) EvaluationOrder() []int {
	index := make(map[string]int, len(p.Modules))
	for i, m := range p.Modules {
		index[m.File] = i
	}
	var order []int
	visited := make([]bool, len(p.Modules))
	var visit func(i int)
	visit = func(i int) {
		if visited[i] {
			return
		}
		visited[i] = true
		for _, file := range p.Modules[i].Imports {
			if j, ok := index[file]; ok {
				visit(j)
			}
		}
		order = append(order, i)
	}
	visit(p.Entry)
	return order
}

// Module returns the module whose File is file, or nil.
func (p *Program) Module(file string) *Module {
	for _, m := range p.Modules {
		if m.File == file {
			return m
		}
	}
	return nil
}

// Validate checks the invariants lowering keeps: the modules are in order
// with an entry among them, each imports only modules of the program, and
// every module-qualified reference names a top-level declaration of a module
// in the program, assigned only if it is a let. Every object type is of one
// of the program's shapes, interned, and each object and array node fits
// the shape or element type it names (check.go). A failure is a bug in
// lowering, not in the TypeScript, so a backend calls this rather than emit
// Go from a program that breaks it.
func (p *Program) Validate() error {
	if len(p.Modules) == 0 {
		return errors.New("the program has no modules")
	}
	if p.Entry < 0 || p.Entry >= len(p.Modules) {
		return fmt.Errorf("the entry is module %d of %d", p.Entry, len(p.Modules))
	}
	// A file is how a module is told apart from the others, so a module alone
	// may go without one; a reference into it then names no module and fails
	// below.
	for i, m := range p.Modules {
		if m.File == "" && len(p.Modules) > 1 {
			return fmt.Errorf("module %d of %d has no file", i, len(p.Modules))
		}
		if i > 0 && p.Modules[i-1].File >= m.File {
			return fmt.Errorf("modules %s and %s are out of order, or the same file twice", p.Modules[i-1].File, m.File)
		}
	}

	errs := p.validateShapes()
	for _, m := range p.Modules {
		for _, file := range m.Imports {
			if p.Module(file) == nil {
				errs = append(errs, fmt.Errorf("%s imports %s, which is not one of the program's modules", m.File, file))
			}
		}
	}
	check := func(module, name string, assigned bool, span Span) {
		if err := p.checkRef(module, name, assigned); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", span, err))
		}
	}
	for _, m := range p.Modules {
		for _, fn := range m.Funcs {
			if err := p.checkParams(fn.Params); err != nil {
				errs = append(errs, fmt.Errorf("%s: %s: %w", fn.Span, fn.Name, err))
			}
			if err := p.checkType(fn.Result); err != nil {
				errs = append(errs, fmt.Errorf("%s: %s's result: %w", fn.Span, fn.Name, err))
			}
			if err := checkReturns(fn.Body, returnType(fn.Result, fn.Async)); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", fn.Name, err))
			}
		}
		Inspect(m, func(node any) {
			if err := p.checkNode(node); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", nodeSpan(node), err))
			}
			switch n := node.(type) {
			case *Ident:
				check(n.Module, n.Name, false, n.Span)
			case *Call:
				check(n.Module, n.Callee, false, n.Span)
			case *Assign:
				check(n.Module, n.Name, true, n.Span)
			case *FuncLit:
				for _, c := range n.Captures {
					check(c.Module, c.Name, false, n.Span)
				}
			}
		})
	}
	return errors.Join(errs...)
}

// checkRef checks one reference to name, qualified by module.
func (p *Program) checkRef(module, name string, assigned bool) error {
	if module == "" {
		return nil
	}
	m := p.Module(module)
	if m == nil {
		return fmt.Errorf("%s refers into %s, which is not one of the program's modules", name, module)
	}
	fn, v := m.topLevel(name)
	switch {
	case fn == nil && v == nil:
		return fmt.Errorf("%s refers to the top level of %s, which declares no %s", name, module, name)
	case assigned && fn != nil:
		return fmt.Errorf("assignment to %s, a function of %s", name, module)
	case assigned && !v.Mutable:
		return fmt.Errorf("assignment to %s, a const of %s", name, module)
	}
	return nil
}

// topLevel returns the function or variable m declares at its top level as
// name: a Func, or a VarDecl directly in Main. A variable declared inside a
// block of top-level code is local to that block.
func (m *Module) topLevel(name string) (*Func, *VarDecl) {
	for _, fn := range m.Funcs {
		if fn.Name == name {
			return fn, nil
		}
	}
	for _, s := range m.Main {
		if d, ok := s.(*VarDecl); ok && d.Name == name {
			return nil, d
		}
	}
	return nil, nil
}

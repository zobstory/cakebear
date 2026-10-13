// Package ir is cakebear's lowered intermediate representation: the contract
// between the TypeScript frontend and whatever backend compiles it.
//
// It imports nothing but the standard library, and that is load-bearing rather
// than tidy. backend/ consumes ir/ and must stay free of any dependency on the
// forked TypeScript compiler, so that swapping the Go emitter for an LLVM one
// touches nothing upstream of this package, and so that backend/ can later be
// extracted to a standalone github.com/zobstory/buildbinary. If ir/ ever
// imports internal/..., both of those properties are gone.
//
// Lowering from the checked AST into these types lives in lower/, which is the
// only package that sees both worlds.
//
// The package is split by what a node is: types.go holds the type surface,
// stmt.go the statements, expr.go the expressions. This file holds the module
// and the declarations around them.
package ir

import "fmt"

// Span locates a node in the original TypeScript source.
//
// Every node carries one. Phase 1 does not report runtime errors, but a
// backend that cannot say which line produced a fault is one that has to be
// retrofitted later, and retrofitting spans through an IR is miserable.
type Span struct {
	File  string
	Start int
	End   int
	Line  int // 1-based
	Col   int // 1-based
}

func (s Span) String() string {
	return fmt.Sprintf("%s:%d:%d", s.File, s.Line, s.Col)
}

// Module is one compiled program.
type Module struct {
	// Name is the source basename, used for the output binary.
	Name string
	// File is the source file's path, for messages the program prints about
	// itself at runtime.
	File string
	// Funcs are the top-level function declarations.
	Funcs []*Func
	// Main is the top-level statement sequence, in source order.
	Main []Stmt
	// Async marks a module with a top-level await. Its top-level code runs as
	// an async body, as an ES module's does: up to the first await at once,
	// the rest as the awaited promises settle.
	Async bool
	// Imports are the files of the program's modules this one imports, in
	// source order: what decides when each module's top-level code runs
	// (Program.EvaluationOrder). An `import type` is erased, as Node and
	// TypeScript both erase it, and so is not here; any other import
	// evaluates its module, as under Node, whether or not what it names is
	// used.
	Imports []string
}

// Func is a top-level function declaration.
type Func struct {
	Name   string
	Params []Param
	// Result is the type a caller gets. For an async function that is the
	// promise, PromiseOf(T), while the Returns in Body carry T itself: an
	// async function's `return v` resolves the promise with v.
	Result Type
	Body   []Stmt
	// Async marks an `async` function. Calling one runs its body up to the
	// first Await before the caller continues, as in JavaScript.
	Async bool
	Span  Span
}

type Param struct {
	Name string
	Type Type
	Span Span
}

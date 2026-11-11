package main

import (
	"strings"
	"sync"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/compiler"
	"github.com/zobstory/cakebear/internal/tsoptions"
	"github.com/zobstory/cakebear/internal/tspath"
	cakebeartypes "github.com/zobstory/cakebear/types"
)

// @types/node and cakebear's own node:http declarations (S1 plan D12).
//
// A server project usually has @types/node, and its node:http can't merge
// with cakebear's: cakebear's interfaces IncomingMessage and ServerResponse
// shadow the classes @types/node's node:http re-exports, so @types/node's own
// http2.d.ts and https.d.ts fail to check (63 errors under 24.19.2), and the
// program sees cakebear's few members where its author wrote against
// @types/node's. Declaring cakebear's to match can't work either: two
// classes don't merge, and an interface merging into @types/node's
// generic Server would need its type parameters.
//
// So a native build whose program has @types/node is made again without
// cakebear's node:http, and lowering recognises @types/node's declarations of
// the same members (lower/typesnode.go). Whether a program has it can't be
// told before it is built: besides `"types": ["node"]`, a package's own
// declarations can bring it in with `/// <reference types="node" />`. The
// second build reparses nothing: reuseHost hands it the first build's files.
//
// tsc mode and the language server append cakebear's declarations to a lib
// file, which can't step aside per project, so there a project with
// @types/node still needs skipLibCheck: a known limitation until packages
// are bound (S4).

// typesNodeDir is where a program's @types/node files live, whatever
// node_modules holds them.
const typesNodeDir = "/node_modules/@types/node/"

// usesTypesNode reports whether p has any of @types/node's files.
func usesTypesNode(p *compiler.Program) bool {
	for _, f := range p.SourceFiles() {
		if strings.Contains(string(f.FileName()), typesNodeDir) {
			return true
		}
	}
	return false
}

// withoutNodeHTTP is config without cakebear's node:http declarations as a
// root file.
func withoutNodeHTTP(config *tsoptions.ParsedCommandLine) *tsoptions.ParsedCommandLine {
	ours := tspath.RootedFilePath(cakebeartypes.NodeHTTPPath())
	names := config.FileNames()
	kept := make([]tspath.RootedFilePath, 0, len(names))
	for _, name := range names {
		if name != ours {
			kept = append(kept, name)
		}
	}
	return config.WithFileNames(kept)
}

// reuseHost parses each file once across the programs one build makes. A
// parsed and bound file is shared between programs as the language server
// shares one between a project's versions: binding marks the file, and
// every checker keeps its own state.
type reuseHost struct {
	compiler.CompilerHost

	mu    sync.Mutex
	files map[ast.SourceFileParseOptions]*ast.SourceFile
}

func newReuseHost(h compiler.CompilerHost) *reuseHost {
	return &reuseHost{CompilerHost: h, files: map[ast.SourceFileParseOptions]*ast.SourceFile{}}
}

func (h *reuseHost) GetSourceFile(opts ast.SourceFileParseOptions) *ast.SourceFile {
	h.mu.Lock()
	f, ok := h.files[opts]
	h.mu.Unlock()
	if ok {
		return f
	}
	f = h.CompilerHost.GetSourceFile(opts)
	h.mu.Lock()
	defer h.mu.Unlock()
	if have, ok := h.files[opts]; ok {
		return have // another goroutine parsed it first: keep one copy
	}
	h.files[opts] = f
	return f
}

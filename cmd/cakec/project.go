package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/collections"
	"github.com/zobstory/cakebear/internal/core"
	"github.com/zobstory/cakebear/internal/tsoptions"
	"github.com/zobstory/cakebear/internal/tspath"
	"github.com/zobstory/cakebear/internal/vfs"
	cakebeartypes "github.com/zobstory/cakebear/types"
)

// Native builds from a tsconfig.json (M3 plan step 2, D3). `cakec build -p`
// names the config, or a directory holding one; `cakec build` with no input
// files looks for one in the current directory and then each directory above
// it, as tsc does. The project's files and compiler options are what tsc would
// use. What to build comes from a top-level "cakebear" block, which tsc
// ignores:
//
//	"cakebear": { "main": "src/server.ts", "output": "bin/server" }
//
// main is the module whose top-level code the binary runs: a native binary has
// one entry point, where tsc needs none. output defaults to main's basename,
// beside the config. Both are relative to the config's directory, like every
// other path in it.
//
// Only the config named, or found, is read for the block: tsc's extends does
// not carry unknown keys over, so a block in a base config is not seen.

// errNoInput is a build with no input files and no tsconfig.json to find them.
var errNoInput = errors.New("missing input file: name a .ts file, or run in a project with a tsconfig.json")

// project is a parsed tsconfig.json and what its "cakebear" block asks for.
type project struct {
	config *tsoptions.ParsedCommandLine
	file   string // the config file
	main   string // absolute
	output string // absolute
}

// configFile returns the tsconfig.json to build: the one -p names, or the
// nearest in cwd or a directory above it.
func configFile(opts buildOptions, cwd string) (string, error) {
	if opts.project == "" {
		for dir := cwd; ; dir = filepath.Dir(dir) {
			path := filepath.Join(dir, "tsconfig.json")
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				return path, nil
			}
			if filepath.Dir(dir) == dir {
				return "", errNoInput
			}
		}
	}

	path := opts.project
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		path = filepath.Join(path, "tsconfig.json")
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return "", fmt.Errorf("-p %s: no tsconfig.json there", opts.project)
	}
	return path, nil
}

// loadProject parses the config at path. Diagnostics are the config's own
// unrecoverable errors, which are reported like type errors; an error is a
// project cakebear can't build, such as one with no "cakebear" block. Paths in
// messages are relative to cwd.
func loadProject(path string, opts buildOptions, fs vfs.FS, cwd string) (*project, []*ast.Diagnostic, error) {
	shown := relativeTo(cwd, path)
	// Command-line options override the config's, as on tsc's command line.
	fromCommandLine := &core.CompilerOptions{}
	if opts.checkers > 0 {
		fromCommandLine.Checkers = &opts.checkers
	}
	name := tspath.RootedFilePathFromAbsolute(tspath.NormalizePath(path))
	config, diags := tsoptions.GetParsedCommandLineOfConfigFile(name, fromCommandLine, nil, fs, nil)
	if len(diags) > 0 {
		return nil, diags, nil
	}

	if err := requireStrict(config.CompilerOptions()); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", shown, err)
	}
	p := &project{config: config, file: path}
	if err := p.readBlock(); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", shown, err)
	}
	main := relativeTo(cwd, p.main)
	if !tspath.HasTSFileExtension(p.main) || tspath.IsDeclarationFileName(p.main) {
		return nil, nil, fmt.Errorf("%s: \"cakebear.main\" is %s; it must be a TypeScript module, not a declaration file", shown, main)
	}
	if !slices.ContainsFunc(config.FileNames(), func(f tspath.RootedFilePath) bool {
		return sameFile(string(f), p.main)
	}) {
		return nil, nil, fmt.Errorf("%s: \"cakebear.main\" is %s, which is not one of the project's files (see its files and include)", shown, main)
	}

	// cakebear's declarations are root files of every native program, as in
	// a build with no config. lower recognises spawn, the conversions and
	// node:http's members by the virtual file declaring them, so they stay
	// files of their own here rather than joining a bundled lib.
	parsed := *config.ParsedConfig
	parsed.FileNames = append(slices.Clip(parsed.FileNames), cakebeartypes.RootFiles()...)
	config.ParsedConfig = &parsed
	return p, nil, nil
}

// requireStrict refuses a project that turns off what cakebear's own
// configuration always has on. Each is an escape hatch, an implicit any or a
// null in any type, whose representation the backend cannot pin down.
// TypeScript's strict defaults to true, so only a config saying false gets
// here.
func requireStrict(o *core.CompilerOptions) error {
	for _, option := range []struct {
		name  string
		value core.Tristate
	}{
		{"strictNullChecks", o.StrictNullChecks},
		{"noImplicitAny", o.NoImplicitAny},
	} {
		if !o.GetStrictOptionValue(option.value) {
			return fmt.Errorf("cakebear needs %s, which this config turns off (directly, or with \"strict\": false)", option.name)
		}
	}
	return nil
}

// readBlock reads and checks the config's "cakebear" block.
func (p *project) readBlock() error {
	var value any
	ok := false
	if raw, isObject := p.config.Raw.(*collections.OrderedMap[string, any]); isObject && raw != nil {
		value, ok = raw.Get("cakebear")
	}
	if !ok {
		hint := ""
		if len(p.config.ExtendedSourceFiles()) > 0 {
			hint = "; a block in a config it extends is not read"
		}
		return fmt.Errorf("no \"cakebear\" block naming the module to build, e.g. \"cakebear\": { \"main\": \"src/main.ts\" }%s", hint)
	}
	block, ok := value.(*collections.OrderedMap[string, any])
	if !ok {
		return errors.New("\"cakebear\" must be an object, e.g. { \"main\": \"src/main.ts\" }")
	}
	for key := range block.Keys() {
		if key != "main" && key != "output" {
			return fmt.Errorf("\"cakebear\" has an unknown key %q; it takes main and output", key)
		}
	}

	dir := filepath.Dir(p.file)
	main, err := blockPath(block, "main", dir)
	if err != nil {
		return err
	}
	if main == "" {
		return errors.New("\"cakebear\" has no \"main\", the module to build")
	}
	output, err := blockPath(block, "output", dir)
	if err != nil {
		return err
	}
	if output == "" {
		output = filepath.Join(dir, strings.TrimSuffix(filepath.Base(main), filepath.Ext(main)))
	}
	p.main, p.output = main, output
	return nil
}

// blockPath reads key from the block as a path relative to dir. A missing key
// is "".
func blockPath(block *collections.OrderedMap[string, any], key, dir string) (string, error) {
	value, ok := block.Get(key)
	if !ok {
		return "", nil
	}
	path, ok := value.(string)
	if !ok || path == "" {
		return "", fmt.Errorf("\"cakebear.%s\" must be a non-empty string", key)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	return filepath.Clean(path), nil
}

// sameFile compares a program's file name with an OS path, the way userFiles
// matches them.
func sameFile(fileName, path string) bool {
	return strings.EqualFold(filepath.ToSlash(fileName), filepath.ToSlash(path))
}

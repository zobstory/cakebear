package main

import (
	"strings"

	"github.com/zobstory/cakebear/internal/bundled"
	"github.com/zobstory/cakebear/internal/collections"
	"github.com/zobstory/cakebear/internal/tsoptions"
	"github.com/zobstory/cakebear/internal/tspath"
	"github.com/zobstory/cakebear/internal/vfs"
	cakebeartypes "github.com/zobstory/cakebear/types"
)

// cakebear's declarations outside native builds (M3 plan D4): spawn, the
// conversions (i32, …) and node:http, for tsc mode and the language server.
// Neither can be given extra root files, so the declarations ride along in a
// lib file every program loads: they are appended to the bundled
// lib.es5.d.ts, which every lib chain with an Array reaches. Go-to-definition
// on spawn then lands in lib.es5.d.ts, a bundled path the language server
// knows how to show.
//
//   - The language server always has them: an editor pointed at cakec is a
//     cakebear user.
//   - tsc mode has them only for a project with a "cakebear" block, so cakec
//     on any other project is byte for byte tsc (TestCakecIsTsc).
//   - Native builds keep their own route, separate root files, because
//     lowering recognises a declaration by the virtual file declaring it.
//
// A script (a file with no import or export) that declares its own global
// spawn or i32 then merges with cakebear's, and gets TS2394 inside the lib. A
// module's own declaration shadows cakebear's instead.

// libAppend serves target with extra appended.
type libAppend struct {
	vfs.FS
	target tspath.RootedFilePath
	extra  string
}

func (f *libAppend) ReadFile(path tspath.RootedFilePath) (string, bool) {
	text, ok := f.FS.ReadFile(path)
	if ok && path == f.target {
		text += f.extra
	}
	return text, ok
}

// withDeclarations is fs with cakebear's declarations in lib.es5.d.ts.
func withDeclarations(fs vfs.FS) vfs.FS {
	return &libAppend{
		FS:     fs,
		target: bundled.LibPath().ResolveFile("lib.es5.d.ts"),
		extra:  cakebeartypes.LibText(),
	}
}

// lspFS is the language server's filesystem. tsc_lsp.go calls it, by the one
// line scripts/copy-tsc-cmd.sh patches in cmd/tsc/lsp.go.
func lspFS(fs vfs.FS) vfs.FS { return withDeclarations(fs) }

// declaresCakebear reports whether the tsc invocation args compiles a project
// whose tsconfig.json has a "cakebear" block. The config is found as tsc finds
// it: -p, else the nearest at or above cwd unless input files say otherwise.
// For --build it is any of the projects named, or the one in cwd. Only those
// configs are read: a block in a base config or a referenced project doesn't
// count, as for `cakec build`.
func declaresCakebear(args []string, fs vfs.FS, cwd tspath.RootedDirectoryPath) bool {
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "-b", "--b", "-build", "--build":
			for _, config := range tsoptions.ParseBuildCommandLine(args, fs, cwd).ResolvedProjectPaths() {
				if hasCakebearBlock(fs, config) {
					return true
				}
			}
			return false
		}
	}

	commandLine := tsoptions.ParseCommandLine(args, fs, cwd)
	options := commandLine.CompilerOptions()
	var config tspath.RootedFilePath
	switch {
	case options.Project != "":
		project := tspath.RootedDirectoryPathFromPath(options.Project)
		if fs.DirectoryExists(project) {
			config = project.ResolveFile("tsconfig.json")
		} else {
			config = tspath.RootedFilePathFromPath(options.Project)
		}
	case !options.IgnoreConfig.IsTrue() || len(commandLine.FileNames()) == 0:
		config = nearestConfig(fs, cwd)
	}
	return config != "" && hasCakebearBlock(fs, config)
}

// nearestConfig is tsc's search: tsconfig.json in dir, or the nearest
// directory above it.
func nearestConfig(fs vfs.FS, dir tspath.RootedDirectoryPath) tspath.RootedFilePath {
	config, _ := dir.ForEachAncestorDirectory(func(ancestor tspath.RootedDirectoryPath) (tspath.RootedFilePath, bool) {
		name := ancestor.ResolveFile("tsconfig.json")
		return name, fs.FileExists(name)
	})
	return config
}

// hasCakebearBlock reports whether the config at path has a top-level
// "cakebear" key. Whether the block is valid is `cakec build`'s concern.
func hasCakebearBlock(fs vfs.FS, path tspath.RootedFilePath) bool {
	text, ok := fs.ReadFile(path)
	if !ok {
		return false
	}
	json, _ := tsoptions.ParseConfigFileTextToJson(path, fs.CaseSensitivity().PathKey(path.AsPath()), text)
	raw, ok := json.(*collections.OrderedMap[string, any])
	if !ok || raw == nil {
		return false
	}
	_, ok = raw.Get("cakebear")
	return ok
}

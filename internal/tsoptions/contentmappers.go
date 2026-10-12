package tsoptions

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/contentmapper"
	"github.com/zobstory/cakebear/internal/core"
	"github.com/zobstory/cakebear/internal/diagnostics"
	"github.com/zobstory/cakebear/internal/module"
	"github.com/zobstory/cakebear/internal/packagejson"
	"github.com/zobstory/cakebear/internal/tspath"
	"github.com/zobstory/cakebear/internal/vfs"
)

// resolveContentMapperManifest locates packageName in node_modules (walking up from the directory of
// containingFile via node module resolution) and reads its package.json to produce the mapper's manifest
// and package directory. It never executes the package. On failure it returns a diagnostic describing why
// the mapper could not be resolved; on success the diagnostic is nil.
type contentMapperResolutionHost struct {
	fs               vfs.FS
	currentDirectory tspath.RootedDirectoryPath
}

func (h *contentMapperResolutionHost) FS() vfs.FS {
	return h.fs
}

func (h *contentMapperResolutionHost) GetCurrentDirectory() tspath.RootedDirectoryPath {
	return h.currentDirectory
}

func resolveContentMapperManifest(fs vfs.FS, containingFile tspath.RootedFilePath, packageName string) (contentmapper.Manifest, tspath.RootedDirectoryPath, *ast.Diagnostic) {
	resolver := module.NewResolver(module.ResolverOptions{
		Host:            &contentMapperResolutionHost{fs: fs, currentDirectory: containingFile.Directory()},
		CompilerOptions: &core.CompilerOptions{ModuleResolution: core.ModuleResolutionKindBundler},
	})
	resolved := resolver.ResolvePackageDirectory(packageName, containingFile, core.ResolutionModeNone, nil)
	if resolved == nil || resolved.ResolvedFileName == "" {
		return contentmapper.Manifest{}, "", ast.NewCompilerDiagnostic(diagnostics.The_content_mapper_package_0_could_not_be_resolved, packageName)
	}
	packageDirectory := tspath.RootedDirectoryPathFromPath(tspath.RootedPath(resolved.ResolvedFileName))

	packageJsonPath := packageDirectory.ResolveFile("package.json")
	contents, ok := fs.ReadFile(packageJsonPath)
	if !ok {
		return contentmapper.Manifest{}, packageDirectory, ast.NewCompilerDiagnostic(diagnostics.The_content_mapper_package_0_could_not_be_resolved, packageName)
	}
	fields, err := packagejson.Parse([]byte(contents))
	if err != nil {
		return contentmapper.Manifest{}, packageDirectory, ast.NewCompilerDiagnostic(diagnostics.The_package_json_of_the_content_mapper_package_0_could_not_be_parsed, packageName)
	}
	name, _ := fields.Name.GetValue()
	if name == "" {
		return contentmapper.Manifest{}, packageDirectory, ast.NewCompilerDiagnostic(diagnostics.The_package_json_of_the_content_mapper_package_0_does_not_specify_a_name, packageName)
	}
	version, _ := fields.Version.GetValue()

	// A content mapper package must declare how to run it: a "typescript.contentMapper" object with a non-empty
	// "exec" array of strings.
	cm, ok := fields.ContentMapper.GetValue()
	if !ok {
		return contentmapper.Manifest{}, packageDirectory, ast.NewCompilerDiagnostic(diagnostics.The_package_json_of_the_content_mapper_package_0_does_not_declare_a_typescript_contentMapper_object, packageName)
	}
	exec, ok := cm.Exec.GetValue()
	if !ok || len(exec) == 0 {
		return contentmapper.Manifest{}, packageDirectory, ast.NewCompilerDiagnostic(diagnostics.The_typescript_contentMapper_exec_of_the_content_mapper_package_0_must_be_a_non_empty_array_of_strings, packageName)
	}
	compilerOptions, _ := cm.CompilerOptions.GetValue()
	dynamicConfig, _ := cm.DynamicConfig.GetValue()
	return contentmapper.Manifest{Name: name, Version: version, Exec: exec, CompilerOptions: compilerOptions, DynamicConfig: dynamicConfig}, packageDirectory, nil
}

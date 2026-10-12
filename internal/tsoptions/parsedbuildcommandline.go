package tsoptions

import (
	"sync"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/core"
	"github.com/zobstory/cakebear/internal/locale"
	"github.com/zobstory/cakebear/internal/tspath"
)

type ParsedBuildCommandLine struct {
	BuildOptions    *core.BuildOptions    `json:"buildOptions"`
	CompilerOptions *core.CompilerOptions `json:"compilerOptions"`
	Projects        []string              `json:"projects"`
	Errors          []*ast.Diagnostic     `json:"errors"`
	Raw             any                   `json:"raw"`

	currentDirectory tspath.RootedDirectoryPath

	resolvedProjectPaths     []tspath.RootedFilePath
	resolvedProjectPathsOnce sync.Once

	locale     locale.Locale
	localeOnce sync.Once
}

func (p *ParsedBuildCommandLine) ResolvedProjectPaths() []tspath.RootedFilePath {
	p.resolvedProjectPathsOnce.Do(func() {
		p.resolvedProjectPaths = core.Map(p.Projects, func(project string) tspath.RootedFilePath {
			return core.ResolveConfigFileNameOfProjectReference(
				p.currentDirectory.ResolveFile(project).AsPath(),
			)
		})
	})
	return p.resolvedProjectPaths
}

func (p *ParsedBuildCommandLine) Locale() locale.Locale {
	p.localeOnce.Do(func() {
		p.locale, _ = locale.Parse(p.CompilerOptions.Locale)
	})
	return p.locale
}

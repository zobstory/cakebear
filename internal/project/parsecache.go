package project

import (
	"github.com/zeebo/xxh3"
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/core"
	"github.com/zobstory/cakebear/internal/parser"
)

type ParseCacheKey struct {
	ast.SourceFileParseOptions
	ScriptKind core.ScriptKind
	Hash       xxh3.Uint128
}

func NewParseCacheKey(
	options ast.SourceFileParseOptions,
	hash xxh3.Uint128,
	scriptKind core.ScriptKind,
) ParseCacheKey {
	return ParseCacheKey{
		SourceFileParseOptions: options,
		Hash:                   hash,
		ScriptKind:             scriptKind,
	}
}

type ParseCache = RefCountCache[ParseCacheKey, *ast.SourceFile, FileHandle]

func NewParseCache(options RefCountCacheOptions) *ParseCache {
	return NewRefCountCache(
		options,
		func(key ParseCacheKey, fh FileHandle) *ast.SourceFile {
			file := parser.ParseSourceFile(key.SourceFileParseOptions, fh.Content(), key.ScriptKind)
			file.Hash = fh.Hash()
			return file
		},
	)
}

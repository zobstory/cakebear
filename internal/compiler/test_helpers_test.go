package compiler_test

import (
	"github.com/zobstory/cakebear/internal/core"
	"github.com/zobstory/cakebear/internal/tspath"
)

func testFileNames(fileNames ...string) []tspath.RootedFilePath {
	return core.Map(fileNames, tspath.RootedFilePathFromNormalized)
}

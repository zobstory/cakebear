package sourcemap

import (
	"github.com/zobstory/cakebear/internal/core"
	"github.com/zobstory/cakebear/internal/tspath"
)

type Source interface {
	Text() string
	FileName() tspath.RootedFilePath
	ECMALineMap() []core.TextPos
}

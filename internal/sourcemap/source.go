package sourcemap

import "github.com/zobstory/cakebear/internal/core"

type Source interface {
	Text() string
	FileName() string
	ECMALineMap() []core.TextPos
}

package sourcemap

import "github.com/lilybw/typescript-go/use-at-your-own-risk/core"

type Source interface {
	Text() string
	FileName() string
	ECMALineMap() []core.TextPos
}

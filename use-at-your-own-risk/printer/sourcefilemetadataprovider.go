package printer

import (
	"github.com/lilybw/typescript-go/use-at-your-own-risk/ast"
	"github.com/lilybw/typescript-go/use-at-your-own-risk/tspath"
)

type SourceFileMetaDataProvider interface {
	GetSourceFileMetaData(path tspath.Path) *ast.SourceFileMetaData
}

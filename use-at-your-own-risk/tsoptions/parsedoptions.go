package tsoptions

import (
	"github.com/lilybw/typescript-go/use-at-your-own-risk/contentmapper"
	"github.com/lilybw/typescript-go/use-at-your-own-risk/core"
)

type ParsedOptions struct {
	CompilerOptions *core.CompilerOptions `json:"compilerOptions"`
	WatchOptions    *core.WatchOptions    `json:"watchOptions"`
	TypeAcquisition *core.TypeAcquisition `json:"typeAcquisition"`

	FileNames         []string                 `json:"fileNames"`
	ProjectReferences []*core.ProjectReference `json:"projectReferences"`
	ContentMappers    []*contentmapper.Mapper  `json:"contentMappers"`
}

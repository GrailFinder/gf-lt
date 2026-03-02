package models

const (
	LoadedMark        = "(loaded) "
	ToolRespMultyType = "multimodel_content"
)

type APIType int

const (
	APITypeChat APIType = iota
	APITypeCompletion
)

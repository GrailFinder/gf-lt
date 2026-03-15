package models

const (
	LoadedMark        = "(loaded) "
	ToolRespMultyType = "multimodel_content"
	DefaultFirstMsg   = "Hello! What can I do for you?"
	BasicSysMsg       = "Large Language Model that helps user with any of his requests."
)

type APIType int

const (
	APITypeChat APIType = iota
	APITypeCompletion
)

package models

const (
	LoadedMark = "(loaded) "
)

type APIType int

const (
	APITypeChat APIType = iota
	APITypeCompletion
)

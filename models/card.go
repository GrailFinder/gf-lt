package models

import "strings"

// https://github.com/malfoyslastname/character-card-spec-v2/blob/main/spec_v2.md
// what a bloat; trim to Role->Msg pair and first msg
type CharCardSpec struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	Personality    string `json:"personality"`
	FirstMes       string `json:"first_mes"`
	Avatar         string `json:"avatar"`
	Chat           string `json:"chat"`
	MesExample     string `json:"mes_example"`
	Scenario       string `json:"scenario"`
	CreateDate     string `json:"create_date"`
	Talkativeness  string `json:"talkativeness"`
	Fav            bool   `json:"fav"`
	Creatorcomment string `json:"creatorcomment"`
	Spec           string `json:"spec"`
	SpecVersion    string `json:"spec_version"`
	Tags           []any  `json:"tags"`
}

type Spec2Wrapper struct {
	Data CharCardSpec `json:"data"`
}

func (c *CharCardSpec) Simplify(userName, fpath string) *CharCard {
	fm := strings.ReplaceAll(strings.ReplaceAll(c.FirstMes, "{{char}}", c.Name), "{{user}}", userName)
	sysPr := strings.ReplaceAll(strings.ReplaceAll(c.Description, "{{char}}", c.Name), "{{user}}", userName)
	return &CharCard{
		SysPrompt: sysPr,
		FirstMsg:  fm,
		Role:      c.Name,
		FilePath:  fpath,
	}
}

type CharCard struct {
	SysPrompt string `json:"sys_prompt"`
	FirstMsg  string `json:"first_msg"`
	Role      string `json:"role"`
	FilePath  string `json:"filepath"`
}

package models

import (
	"encoding/json"
	"time"
)

type Chat struct {
	ID        uint32    `db:"id" json:"id"`
	Name      string    `db:"name" json:"name"`
	Msgs      string    `db:"msgs" json:"msgs"` // []RoleMsg to string json
	Agent     string    `db:"agent" json:"agent"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

func (c *Chat) ToHistory() ([]RoleMsg, error) {
	resp := []RoleMsg{}
	if err := json.Unmarshal([]byte(c.Msgs), &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

/*
memories should have two key system
to be able to store different perspectives
agent -> topic -> data
agent is somewhat similar to a char
*/
type Memory struct {
	Agent     string    `db:"agent" json:"agent"`
	Topic     string    `db:"topic" json:"topic"`
	Mind      string    `db:"mind" json:"mind"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

// vector models

type VectorRow struct {
	Embeddings []float32 `db:"embeddings" json:"embeddings"`
	Slug       string    `db:"slug" json:"slug"`
	RawText    string    `db:"raw_text" json:"raw_text"`
	Distance   float32   `db:"distance" json:"distance"`
	FileName   string    `db:"filename" json:"filename"`
}

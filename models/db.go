package models

import (
	"encoding/json"
	"time"
)

type Chat struct {
	ID        uint32    `db:"id" json:"id"`
	Name      string    `db:"name" json:"name"`
	Msgs      string    `db:"msgs" json:"msgs"` // []MessagesStory to string json
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

func (c Chat) ToHistory() ([]MessagesStory, error) {
	resp := []MessagesStory{}
	if err := json.Unmarshal([]byte(c.Msgs), &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

type Memory struct {
	Topic string `db:"topic" json:"topic"`
	Data  string `db:"data" json:"data"`
}

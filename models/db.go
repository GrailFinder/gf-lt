package models

import "time"

type Chat struct {
	ID        uint32    `db:"id" json:"id"`
	Name      string    `db:"name" json:"name"`
	Msgs      string    `db:"msgs" json:"msgs"` // []MessagesStory to string json
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

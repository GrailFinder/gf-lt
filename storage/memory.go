package storage

import "gf-lt/models"

type Memories interface {
	Memorise(m *models.Memory) (*models.Memory, error)
	Recall(agent, topic string) (string, error)
	RecallTopics(agent string) ([]string, error)
}

func (p ProviderSQL) Memorise(m *models.Memory) (*models.Memory, error) {
	query := `
        INSERT INTO memories (agent, topic, mind)
        VALUES (:agent, :topic, :mind)
        ON CONFLICT (agent, topic) DO UPDATE
        SET mind = excluded.mind,
            updated_at = CURRENT_TIMESTAMP
        RETURNING *;`
	stmt, err := p.db.PrepareNamed(query)
	if err != nil {
		p.logger.Error("failed to prepare stmt", "query", query, "error", err)
		return nil, err
	}
	defer stmt.Close()
	var memory models.Memory
	err = stmt.Get(&memory, m)
	if err != nil {
		p.logger.Error("failed to upsert memory", "query", query, "error", err)
		return nil, err
	}
	return &memory, nil
}

func (p ProviderSQL) Recall(agent, topic string) (string, error) {
	query := "SELECT mind FROM memories WHERE agent = $1 AND topic = $2"
	var mind string
	err := p.db.Get(&mind, query, agent, topic)
	if err != nil {
		p.logger.Error("failed to get memory", "query", query, "error", err)
		return "", err
	}
	return mind, nil
}

func (p ProviderSQL) RecallTopics(agent string) ([]string, error) {
	query := "SELECT DISTINCT topic FROM memories WHERE agent = $1"
	var topics []string
	err := p.db.Select(&topics, query, agent)
	if err != nil {
		p.logger.Error("failed to get topics", "query", query, "error", err)
		return nil, err
	}
	return topics, nil
}

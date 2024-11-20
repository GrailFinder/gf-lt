package storage

import "elefant/models"

type Memories interface {
	Memorise(m *models.Memory) (*models.Memory, error)
	Recall(agent, topic string) (string, error)
	RecallTopics(agent string) ([]string, error)
}

func (p ProviderSQL) Memorise(m *models.Memory) (*models.Memory, error) {
	query := "INSERT INTO memories (agent, topic, mind) VALUES (:agent, :topic, :mind) RETURNING *;"
	stmt, err := p.db.PrepareNamed(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	var memory models.Memory
	err = stmt.Get(&memory, m)
	if err != nil {
		return nil, err
	}
	return &memory, nil
}

func (p ProviderSQL) Recall(agent, topic string) (string, error) {
	query := "SELECT mind FROM memories WHERE agent = $1 AND topic = $2"
	var mind string
	err := p.db.Get(&mind, query, agent, topic)
	if err != nil {
		return "", err
	}
	return mind, nil
}

func (p ProviderSQL) RecallTopics(agent string) ([]string, error) {
	query := "SELECT DISTINCT topic FROM memories WHERE agent = $1"
	var topics []string
	err := p.db.Select(&topics, query, agent)
	if err != nil {
		return nil, err
	}
	return topics, nil
}

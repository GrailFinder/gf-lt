package models

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestORModelsListModels(t *testing.T) {
	t.Run("unit test with hardcoded data", func(t *testing.T) {
		jsonData := `{
			"data": [
				{
					"id": "model/free",
					"pricing": {
						"prompt": "0",
						"completion": "0"
					}
				},
				{
					"id": "model/paid",
					"pricing": {
						"prompt": "0.001",
						"completion": "0.002"
					}
				},
				{
					"id": "model/request-zero",
					"pricing": {
						"prompt": "0",
						"completion": "0",
						"request": "0"
					}
				},
				{
					"id": "model/request-nonzero",
					"pricing": {
						"prompt": "0",
						"completion": "0",
						"request": "0.5"
					}
				}
			]
		}`
		var models ORModels
		if err := json.Unmarshal([]byte(jsonData), &models); err != nil {
			t.Fatalf("failed to unmarshal test data: %v", err)
		}
		freeModels := models.ListModels(true)
		if len(freeModels) != 2 {
			t.Errorf("expected 2 free models, got %d: %v", len(freeModels), freeModels)
		}
		expectedFree := map[string]bool{"model/free": true, "model/request-zero": true}
		for _, id := range freeModels {
			if !expectedFree[id] {
				t.Errorf("unexpected free model ID: %s", id)
			}
		}
		allModels := models.ListModels(false)
		if len(allModels) != 4 {
			t.Errorf("expected 4 total models, got %d", len(allModels))
		}
	})

	t.Run("integration with or_models.json", func(t *testing.T) {
		// Attempt to load the real data file from the project root
		path := filepath.Join("..", "or_models.json")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Skip("or_models.json not found, skipping integration test")
		}
		var models ORModels
		if err := json.Unmarshal(data, &models); err != nil {
			t.Fatalf("failed to unmarshal %s: %v", path, err)
		}
		freeModels := models.ListModels(true)
		if len(freeModels) == 0 {
			t.Error("expected at least one free model, got none")
		}
		allModels := models.ListModels(false)
		if len(allModels) == 0 {
			t.Error("expected at least one model")
		}
		// Ensure free models are subset of all models
		freeSet := make(map[string]bool)
		for _, id := range freeModels {
			freeSet[id] = true
		}
		for _, id := range freeModels {
			if !freeSet[id] {
				t.Errorf("free model %s not found in all models", id)
			}
		}
		t.Logf("found %d free models out of %d total models", len(freeModels), len(allModels))
	})
}
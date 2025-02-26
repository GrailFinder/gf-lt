package extra

import (
	"testing"
)

func TestPrepCards(t *testing.T) {
	// Run the function to get the murder combination and player cards
	roundInfo := CluedoPrepCards([]string{"{{user}}", "{{char}}", "{{char2}}"})
	// Create a map to track all distributed cards
	distributedCards := make(map[string]bool)
	// Check that the murder combination cards are not distributed to players
	murderCards := []string{roundInfo.Answer.Murderer, roundInfo.Answer.Weapon, roundInfo.Answer.Room}
	for _, card := range murderCards {
		if distributedCards[card] {
			t.Errorf("Murder card %s was distributed to a player", card)
		}
	}
	// Check each player's cards
	for player, cards := range roundInfo.PlayersCards {
		for _, card := range cards {
			// Ensure the card is not part of the murder combination
			for _, murderCard := range murderCards {
				if card == murderCard {
					t.Errorf("Player %s has a murder card: %s", player, card)
				}
			}
			// Ensure the card is unique and not already distributed
			if distributedCards[card] {
				t.Errorf("Card %s is duplicated in player %s's hand", card, player)
			}
			distributedCards[card] = true
		}
	}
	// Verify that all non-murder cards are distributed
	allCards := append(append([]string{}, rooms...), weapons...)
	allCards = append(allCards, people...)
	for _, card := range allCards {
		isMurderCard := false
		for _, murderCard := range murderCards {
			if card == murderCard {
				isMurderCard = true
				break
			}
		}
		if !isMurderCard && !distributedCards[card] {
			t.Errorf("Card %s was not distributed to any player", card)
		}
	}
}

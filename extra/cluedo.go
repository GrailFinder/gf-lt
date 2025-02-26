package extra

import (
	"math/rand"
	"strings"
)

var (
	rooms   = []string{"HALL", "LOUNGE", "DINING ROOM", "KITCHEN", "BALLROOM", "CONSERVATORY", "BILLIARD ROOM", "LIBRARY", "STUDY"}
	weapons = []string{"CANDLESTICK", "DAGGER", "LEAD PIPE", "REVOLVER", "ROPE", "SPANNER"}
	people  = []string{"Miss Scarlett", "Colonel Mustard", "Mrs. White", "Reverend Green", "Mrs. Peacock", "Professor Plum"}
)

type MurderTrifecta struct {
	Murderer string
	Weapon   string
	Room     string
}

type CluedoRoundInfo struct {
	Answer       MurderTrifecta
	PlayersCards map[string][]string
}

func (c *CluedoRoundInfo) GetPlayerCards(player string) string {
	// maybe format it a little
	return "cards of " + player + "are " + strings.Join(c.PlayersCards[player], ",")
}

func CluedoPrepCards(playerOrder []string) *CluedoRoundInfo {
	res := &CluedoRoundInfo{}
	// Select murder components
	trifecta := MurderTrifecta{
		Murderer: people[rand.Intn(len(people))],
		Weapon:   weapons[rand.Intn(len(weapons))],
		Room:     rooms[rand.Intn(len(rooms))],
	}
	// Collect non-murder cards
	var notInvolved []string
	for _, room := range rooms {
		if room != trifecta.Room {
			notInvolved = append(notInvolved, room)
		}
	}
	for _, weapon := range weapons {
		if weapon != trifecta.Weapon {
			notInvolved = append(notInvolved, weapon)
		}
	}
	for _, person := range people {
		if person != trifecta.Murderer {
			notInvolved = append(notInvolved, person)
		}
	}
	// Shuffle and distribute cards
	rand.Shuffle(len(notInvolved), func(i, j int) {
		notInvolved[i], notInvolved[j] = notInvolved[j], notInvolved[i]
	})
	players := map[string][]string{}
	cardsPerPlayer := len(notInvolved) / len(playerOrder)
	// playerOrder := []string{"{{user}}", "{{char}}", "{{char2}}"}
	for i, player := range playerOrder {
		start := i * cardsPerPlayer
		end := (i + 1) * cardsPerPlayer
		if end > len(notInvolved) {
			end = len(notInvolved)
		}
		players[player] = notInvolved[start:end]
	}
	res.Answer = trifecta
	res.PlayersCards = players
	return res
}

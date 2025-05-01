package extra

import "math/rand"

var (
	chars = []string{"Shrek", "Garfield", "Jack the Ripper"}
)

func GetRandomChar() string {
	return chars[rand.Intn(len(chars))]
}

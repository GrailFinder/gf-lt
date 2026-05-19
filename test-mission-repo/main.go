package main

import (
	"fmt"
	"os"
)

// Greet returns a greeting message for the given name.
// TODO: Add validation for empty input.
func Greet(name string) string {
	return fmt.Sprintf("Hello, %s!", name)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: greeter <name>")
		os.Exit(1)
	}
	name := os.Args[1]
	fmt.Println(Greet(name))
}

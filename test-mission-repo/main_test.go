package main

import "testing"

func TestGreetNormal(t *testing.T) {
	got := Greet("World")
	want := "Hello, World!"
	if got != want {
		t.Errorf("Greet(%q) = %q, want %q", "World", got, want)
	}
}

func TestGreetName(t *testing.T) {
	got := Greet("Alice")
	want := "Hello, Alice!"
	if got != want {
		t.Errorf("Greet(%q) = %q, want %q", "Alice", got, want)
	}
}

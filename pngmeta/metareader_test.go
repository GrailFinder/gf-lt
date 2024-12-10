package pngmeta

import (
	"fmt"
	"testing"
)

func TestReadMeta(t *testing.T) {
	cases := []struct {
		Filename string
	}{
		{
			Filename: "../sysprompts/default_Seraphina.png",
		},
		{
			Filename: "../sysprompts/llama.png",
		},
	}
	for i, tc := range cases {
		t.Run(fmt.Sprintf("test_%d", i), func(t *testing.T) {
			// Call the readMeta function
			pembed, err := extractChar(tc.Filename)
			if err != nil {
				t.Errorf("Expected no error, but got %v", err)
			}
			v, err := pembed.GetDecodedValue()
			if err != nil {
				t.Errorf("Expected no error, but got %v\n", err)
			}
			fmt.Printf("%+v\n", v.Simplify("Adam", tc.Filename))
		})
	}
}

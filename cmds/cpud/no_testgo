package main

import (
	"os"
	"reflect"
	"testing"
)

func TestBuildCmd(t *testing.T) {
	var tests = []struct {
		in    string
		shell string
		out   []string
	}{
		{"", "", []string{"/bin/sh"}},
	}

	for _, tt := range tests {
		if err := os.Setenv("SHELL", tt.shell); err != nil {
			t.Fatalf("os.Setenv(%q): %v != nil", tt.shell, err)
		}
		f := buildCmd(tt.in)
		if !reflect.DeepEqual(f, tt.in) {
			t.Fatalf("buildCmd(%q) with SHELL %q: %q != %q", tt.in, tt.shell, f, tt.out)
		}
	}
}

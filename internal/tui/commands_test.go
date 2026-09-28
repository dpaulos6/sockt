package tui

import "testing"

func TestSlashCandidates(t *testing.T) {
	cases := []struct {
		input string
		open  bool
		names []string
	}{
		{input: "/", open: true, names: []string{"/account", "/help", "/update", "/exit"}},
		{input: "/a", open: true, names: []string{"/account"}},
		{input: "/UP", open: true, names: []string{"/update"}},
		{input: "  /he", open: true, names: []string{"/help"}},
		{input: "/test", open: true},
		{input: "hello", open: false},
		{input: "/help extra", open: false},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, open := slashCandidates(tc.input)
			if open != tc.open || len(got) != len(tc.names) {
				t.Fatalf("slashCandidates(%q) = %+v, %v; want %v, %v", tc.input, got, open, tc.names, tc.open)
			}
			for i, command := range got {
				if command.Name != tc.names[i] {
					t.Errorf("command %d = %q, want %q", i, command.Name, tc.names[i])
				}
			}
		})
	}
}

func TestSlashPrefix(t *testing.T) {
	for input, want := range map[string]bool{"/test": true, " /TEST extra": true, "hello": false, "hi /help": false, "": false} {
		if got := hasSlashPrefix(input); got != want {
			t.Errorf("hasSlashPrefix(%q) = %v, want %v", input, got, want)
		}
	}
}

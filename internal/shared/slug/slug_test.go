package slug

import (
	"fmt"
	"strings"
	"testing"
)

func TestMake(t *testing.T) {
	cases := map[string]string{
		"Broiler Chicken":        "broiler-chicken",
		"Duck (Duckfowl)":        "duck-duckfowl",
		"Maize (Corn)":           "maize-corn",
		"Guinea Corn (Sorghum)":  "guinea-corn-sorghum",
		"Local (Native) Chicken": "local-native-chicken",
		"Rice (Paddy)":           "rice-paddy",
		"  Padded  Name  ":       "padded-name",
		"100kg Bag":              "100kg-bag",
		"Multiple---Dashes":      "multiple-dashes",
	}

	for input, want := range cases {
		if got := Make(input); got != want {
			t.Errorf("Make(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestMakeTruncatesLongNames(t *testing.T) {
	got := Make(strings.Repeat("a", 200))

	if len(got) > maxLength {
		t.Errorf("Make() produced a %d character slug, want at most %d", len(got), maxLength)
	}
	if strings.HasSuffix(got, "-") {
		t.Error("Make() left a trailing dash after truncation")
	}
}

func TestMakeUniqueAppendsSuffix(t *testing.T) {
	taken := map[string]bool{"goat": true, "goat-2": true}

	got, err := MakeUnique("Goat", func(candidate string) (bool, error) {
		return taken[candidate], nil
	})
	if err != nil {
		t.Fatalf("MakeUnique() returned error: %v", err)
	}

	if got != "goat-3" {
		t.Errorf("MakeUnique() = %q, want %q", got, "goat-3")
	}
}

func TestMakeUniqueRejectsEmptySlug(t *testing.T) {
	if _, err := MakeUnique("!!!", func(string) (bool, error) { return false, nil }); err == nil {
		t.Error("MakeUnique() accepted a name that produces no slug")
	}
}

func TestMakeUniquePropagatesLookupError(t *testing.T) {
	want := fmt.Errorf("database unavailable")

	if _, err := MakeUnique("Goat", func(string) (bool, error) { return false, want }); err == nil {
		t.Error("MakeUnique() swallowed the lookup error")
	}
}

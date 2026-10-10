// Package slug turns product and category names into URL-safe identifiers.
package slug

import (
	"fmt"
	"strings"
	"unicode"
)

// maxLength keeps slugs readable and within index key limits.
const maxLength = 80

// Make converts a name into a lowercase, hyphen-separated slug.
//
// Everything that is not a letter or digit becomes a separator, so
// "Duck (Duckfowl)" becomes "duck-duckfowl" and "Maize (Corn)" becomes
// "maize-corn".
func Make(name string) string {
	var b strings.Builder
	b.Grow(len(name))

	lastWasDash := true // leading dashes are suppressed

	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastWasDash = false
		case !lastWasDash:
			b.WriteByte('-')
			lastWasDash = true
		}
	}

	result := strings.Trim(b.String(), "-")
	if len(result) > maxLength {
		result = strings.Trim(result[:maxLength], "-")
	}

	return result
}

// MakeUnique appends a numeric suffix until exists reports the slug is free.
// The caller still relies on the unique index as the real guarantee; this just
// avoids a pointless round trip for the common case.
func MakeUnique(name string, exists func(candidate string) (bool, error)) (string, error) {
	base := Make(name)
	if base == "" {
		return "", fmt.Errorf("slug: %q produces an empty slug", name)
	}

	candidate := base
	for attempt := 2; attempt <= 50; attempt++ {
		taken, err := exists(candidate)
		if err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, attempt)
	}

	return "", fmt.Errorf("slug: could not derive a unique slug from %q", name)
}

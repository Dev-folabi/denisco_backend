package infrastructure

import "regexp"

// regexpQuote escapes user input before it reaches a MongoDB $regex, so a
// search for "100kg (bag)" is matched literally instead of being interpreted
// as a pattern.
func regexpQuote(input string) string {
	return regexp.QuoteMeta(input)
}

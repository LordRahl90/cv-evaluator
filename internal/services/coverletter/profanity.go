package coverletter

import (
	"regexp"
	"strings"
)

// profanity is the list of swear words a cover letter must never contain.
// It is short on purpose: the prompt already asks for a formal letter, and
// this list is the guarantee behind it, not a content filter. Words are
// matched case-insensitively on word boundaries, so "Scunthorpe" or
// "assess" never match. Add inflections explicitly.
var profanity = []string{
	"arse", "arsehole", "ass", "asshole", "bastard", "bitch", "bitching",
	"bloody", "bollocks", "bullshit", "crap", "crappy", "cunt", "damn",
	"damned", "dick", "dickhead", "fuck", "fucked", "fucker", "fucking",
	"goddamn", "hell", "motherfucker", "piss", "pissed", "prick", "shit",
	"shitty", "slut", "twat", "wanker", "whore", "wtf",
}

// wordListPattern returns a case-insensitive regexp that matches any of
// words as a whole word. Spaces in a phrase match any run of whitespace.
func wordListPattern(words []string) *regexp.Regexp {
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = strings.ReplaceAll(regexp.QuoteMeta(w), " ", `\s+`)
	}
	return regexp.MustCompile(`(?i)\b(` + strings.Join(quoted, "|") + `)\b`)
}

var profanityPattern = wordListPattern(profanity)

package match

import (
	"strings"
	"unicode"
)

// Minimums a guest match must meet before it reaches the paid model. They
// are well below any real job posting or CV, and the web app checks the job
// description ones too (web/src/views/GuestQuickMatchView.vue).
const (
	minGuestJobWords = 40
	minGuestJobChars = 200
	minGuestCVWords  = 50

	// minDistinctJobWords and maxRepeatShare catch text that is long enough
	// but says nothing: one word or character over and over.
	minDistinctJobWords = 15
	maxRepeatShare      = 0.3
)

// jobDescriptionProblem says why a trimmed job description is not worth
// scoring, or returns "" when it is.
func jobDescriptionProblem(text string) string {
	fields := strings.Fields(text)
	if len(fields) < minGuestJobWords || len([]rune(text)) < minGuestJobChars {
		return "The job description is too short. Paste the full posting, at least 40 words."
	}
	if looksRepeated(fields) {
		return "The job description does not look like a job posting. Paste the posting's text: title, responsibilities and requirements."
	}
	return ""
}

// looksRepeated reports whether fields have no letters, too few different
// words, or one word or character making up much of the text.
func looksRepeated(fields []string) bool {
	words := map[string]int{}
	chars := map[rune]int{}
	var total, letters, alnum int
	for _, f := range fields {
		for _, r := range f {
			if unicode.IsLetter(r) {
				letters++
			}
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				alnum++
				chars[unicode.ToLower(r)]++
			}
		}
		w := strings.ToLower(strings.TrimFunc(f, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }))
		if w != "" {
			words[w]++
			total++
		}
	}
	if letters == 0 || len(words) < minDistinctJobWords {
		return true
	}
	return share(words, total) > maxRepeatShare || share(chars, alnum) > maxRepeatShare
}

// share is the fraction of total taken by the most common key in counts.
func share[K comparable](counts map[K]int, total int) float64 {
	top := 0
	for _, n := range counts {
		top = max(top, n)
	}
	return float64(top) / float64(total)
}

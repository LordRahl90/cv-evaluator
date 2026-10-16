package prompts

import (
	"regexp"
	"strings"
)

// promptTags matches the tags the prompts wrap untrusted text in, opening
// or closing, in any letter case and with stray whitespace, such as
// "</cv>", "<JOB_DESCRIPTION>" or "< /Evaluation >".
var promptTags = regexp.MustCompile(`(?i)<\s*/?\s*(job_description|cv|evaluation)\s*>`)

// StripTags removes the prompts' own tags (<job_description>, <cv> and
// <evaluation>) from scraped, uploaded or client-supplied text, so the text
// can't close its tag early and smuggle text into the instructions.
func StripTags(text string) string {
	return promptTags.ReplaceAllString(text, "")
}

// TagCV wraps CV text in <cv> tags, after stripping any tags inside it.
func TagCV(text string) string {
	return "<cv>\n" + strings.TrimSpace(StripTags(text)) + "\n</cv>"
}

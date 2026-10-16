package prompts

import "strings"

// promptTags are the tags the prompts wrap untrusted text in.
var promptTags = strings.NewReplacer(
	"</job_description>", "", "<job_description>", "",
	"</cv>", "", "<cv>", "",
	"</JOB_DESCRIPTION>", "", "<JOB_DESCRIPTION>", "",
	"</CV>", "", "<CV>", "",
)

// StripTags removes the prompts' own tags from scraped or uploaded text, so
// a job posting or CV can't close its tag early and smuggle text into the
// instructions.
func StripTags(text string) string {
	return promptTags.Replace(text)
}

// TagCV wraps CV text in <cv> tags, after stripping any tags inside it.
func TagCV(text string) string {
	return "<cv>\n" + strings.TrimSpace(StripTags(text)) + "\n</cv>"
}

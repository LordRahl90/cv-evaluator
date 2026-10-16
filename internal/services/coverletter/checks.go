package coverletter

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"cv-evaluator/internal/entities"
)

// Names of the checks a letter must pass. They are stable: they are logged
// when a letter is rejected, and never with the letter's text.
const (
	// CheckFormat fails when the LLM's answer is not the JSON object asked
	// for; the other checks need a decoded letter.
	CheckFormat      = "format"
	CheckSubject     = "subject"
	CheckWordCount   = "word_count"
	CheckProfanity   = "profanity"
	CheckCliche      = "cliche"
	CheckURL         = "url"
	CheckContact     = "contact_details"
	CheckPlaceholder = "placeholder"
	CheckExclamation = "exclamation_mark"
	CheckSalutation  = "salutation"
	CheckSignOff     = "sign_off"
)

// maxSubjectChars caps the subject line.
const maxSubjectChars = 120

// Violation is one failed check. Message tells the LLM how to fix it, so it
// may quote the offending text; log Check only.
type Violation struct {
	Check   string
	Message string
}

var (
	clichePattern = wordListPattern(entities.CoverLetterBannedPhrases)
	emailPattern  = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9\-]+(\.[A-Za-z0-9\-]+)*\.[A-Za-z]{2,}`)
	// urlPattern matches links with a scheme or "www.", and bare .com
	// domains. Other bare domains are left alone: "ASP.NET" or "Socket.io"
	// are technologies, not links.
	urlPattern = regexp.MustCompile(`(?i)\b(https?://|ftp://|www\.)\S*|\b[a-z0-9\-]+\.com\b`)
	// phonePattern matches runs of at least 8 digits with the usual
	// separators; phoneNumber then rejects year ranges.
	phonePattern     = regexp.MustCompile(`\+?\(?\d[\d ().\-/]{6,}\d`)
	yearRangePattern = regexp.MustCompile(`^\d{4}\s*[-/]\s*\d{4}$`)
	// placeholderPattern matches brackets of any kind and the usual
	// template leftovers.
	placeholderPattern = regexp.MustCompile(`(?i)[\[\]{}<>]|\b(lorem ipsum|xxx+|tbd|your name|company name|job title|hiring manager's name)\b`)
	salutationPattern  = regexp.MustCompile(`^Dear [^\n,]{2,80},$`)
	closingPattern     = regexp.MustCompile(`(?i)^(kind regards|best regards|warm regards|regards|sincerely|yours sincerely|yours faithfully|with kind regards|respectfully),$`)
)

// Check runs every check on a letter written for candidate and returns the
// violations, in a stable order. A letter passes when there are none.
func Check(letter Letter, candidate Candidate) []Violation {
	var out []Violation
	add := func(check, format string, args ...any) {
		out = append(out, Violation{Check: check, Message: fmt.Sprintf(format, args...)})
	}

	subject := strings.TrimSpace(letter.Subject)
	switch {
	case subject == "":
		add(CheckSubject, "the subject is missing")
	case strings.ContainsAny(subject, "\r\n"):
		add(CheckSubject, "the subject must be a single line")
	case len([]rune(subject)) > maxSubjectChars:
		add(CheckSubject, "the subject must be at most %d characters", maxSubjectChars)
	}

	if n := WordCount(letter.Letter); n < entities.CoverLetterMinWords || n > entities.CoverLetterMaxWords {
		add(CheckWordCount, "the letter has %d words; write %d-%d words", n, entities.CoverLetterTargetMinWords, entities.CoverLetterTargetMaxWords)
	}

	// The content checks cover the subject as well as the letter.
	text := subject + "\n" + letter.Letter
	if found := uniqueMatches(profanityPattern, text); len(found) > 0 {
		add(CheckProfanity, "remove the profanity: %s", quoteAll(found))
	}
	if found := uniqueMatches(clichePattern, text); len(found) > 0 {
		add(CheckCliche, "do not use these words or phrases: %s", quoteAll(found))
	}

	var foreign []string
	for _, email := range emailPattern.FindAllString(text, -1) {
		if candidate.Email == "" || !strings.EqualFold(email, strings.TrimSpace(candidate.Email)) {
			foreign = append(foreign, email)
		}
	}
	for _, phone := range phonePattern.FindAllString(text, -1) {
		if phoneNumber(phone) && !samePhone(phone, candidate.Phone) {
			foreign = append(foreign, strings.TrimSpace(phone))
		}
	}
	if len(foreign) > 0 {
		add(CheckContact, "remove these email addresses or phone numbers: %s", quoteAll(foreign))
	}

	// Emails are reported above, so they are not reported as links too.
	if found := uniqueMatches(urlPattern, emailPattern.ReplaceAllString(text, " ")); len(found) > 0 {
		add(CheckURL, "remove the links: %s", quoteAll(found))
	}
	if found := uniqueMatches(placeholderPattern, text); len(found) > 0 {
		add(CheckPlaceholder, "remove the brackets and placeholders: %s", quoteAll(found))
	}
	if strings.Contains(text, "!") {
		add(CheckExclamation, "remove every exclamation mark")
	}

	lines := nonEmptyLines(letter.Letter)
	if len(lines) == 0 || !salutationPattern.MatchString(lines[0]) {
		add(CheckSalutation, `start the letter with the salutation "Dear Hiring Manager," on its own line`)
	}
	if !signedOff(lines, candidate.Name) {
		add(CheckSignOff, `end the letter with "Kind regards," on its own line and the candidate's full name on the last line`)
	}
	return out
}

// Names returns the names of the failed checks.
func Names(violations []Violation) []string {
	names := make([]string, len(violations))
	for i, v := range violations {
		names[i] = v.Check
	}
	return names
}

// WordCount counts the whitespace-separated words of text.
func WordCount(text string) int {
	return len(strings.Fields(text))
}

// signedOff reports whether the last line is the candidate's name and the
// line before it a closing such as "Kind regards,". Without a name, any
// last line after a closing will do.
func signedOff(lines []string, name string) bool {
	if len(lines) < 3 {
		return false
	}
	last, closing := lines[len(lines)-1], lines[len(lines)-2]
	if !closingPattern.MatchString(closing) {
		return false
	}
	if name = strings.Join(strings.Fields(name), " "); name == "" {
		return true
	}
	return strings.EqualFold(strings.Join(strings.Fields(last), " "), name)
}

// phoneNumber reports whether a phonePattern match is a phone number: at
// least 8 digits, and not a range of years such as "2017-2021".
func phoneNumber(s string) bool {
	s = strings.TrimSpace(s)
	return len(digits(s)) >= 8 && !yearRangePattern.MatchString(s)
}

// samePhone reports whether found is the user's own phone number, with or
// without a country code.
func samePhone(found, own string) bool {
	f, o := digits(found), digits(own)
	if len(f) < 8 || len(o) < 8 {
		return false
	}
	return strings.HasSuffix(f, o) || strings.HasSuffix(o, f)
}

func digits(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
}

func nonEmptyLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// uniqueMatches returns the distinct matches of re in text, lower-cased, in
// order of appearance.
func uniqueMatches(re *regexp.Regexp, text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range re.FindAllString(text, -1) {
		m = strings.ToLower(strings.Join(strings.Fields(m), " "))
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

func quoteAll(items []string) string {
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = fmt.Sprintf("%q", item)
	}
	return strings.Join(quoted, ", ")
}

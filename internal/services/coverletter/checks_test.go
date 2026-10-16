package coverletter

import (
	"strings"
	"testing"

	"cv-evaluator/internal/entities"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheck_ValidLetterPasses(t *testing.T) {
	letter := validLetter()
	require.GreaterOrEqual(t, letter.WordCount, entities.CoverLetterTargetMinWords)
	require.LessOrEqual(t, letter.WordCount, entities.CoverLetterTargetMaxWords)
	assert.Empty(t, Check(letter, testCandidate))
}

// withSentence returns the valid letter with sentence appended to the
// first paragraph.
func withSentence(sentence string) Letter {
	paragraphs := append([]string{}, testParagraphs...)
	paragraphs[0] += " " + sentence
	l := validLetter()
	l.Letter = testLetterBody(paragraphs...)
	return l
}

func TestCheck_EachRule(t *testing.T) {
	long := strings.Repeat("Go services at scale. ", 40)
	tests := []struct {
		name    string
		letter  Letter
		check   string
		message string
	}{
		{"missing subject", Letter{Subject: " ", Letter: validLetter().Letter}, CheckSubject, "the subject is missing"},
		{"multi-line subject", Letter{Subject: "Application\nfor a job", Letter: validLetter().Letter}, CheckSubject, "single line"},
		{"long subject", Letter{Subject: strings.Repeat("a", 121), Letter: validLetter().Letter}, CheckSubject, "at most 120"},
		{"too short", Letter{Subject: testSubject, Letter: testLetterBody(testParagraphs[0])}, CheckWordCount, "write 250-350 words"},
		{"too long", withSentence(long), CheckWordCount, "write 250-350 words"},
		{"profanity", withSentence("I fixed that damn queue."), CheckProfanity, `"damn"`},
		{"profanity in any case", withSentence("It was BULLSHIT."), CheckProfanity, `"bullshit"`},
		{"profanity in the subject", Letter{Subject: "Shit application", Letter: validLetter().Letter}, CheckProfanity, `"shit"`},
		{"cliche", withSentence("I am passionate about logistics."), CheckCliche, `"passionate"`},
		{"cliche phrase across a line break", withSentence("I am a perfect\nfit."), CheckCliche, `"perfect fit"`},
		{"excited to", withSentence("I am Excited to apply."), CheckCliche, `"excited to"`},
		{"rockstar", withSentence("I am a rockstar."), CheckCliche, `"rockstar"`},
		{"url with scheme", withSentence("See https://github.com/jane."), CheckURL, "https://github.com/jane."},
		{"www url", withSentence("See www.janedoe.dev for more."), CheckURL, "www.janedoe.dev"},
		{"bare com domain", withSentence("See janedoe.com for more."), CheckURL, "janedoe.com"},
		{"other email", withSentence("Write to hr@parcelhub.dk."), CheckContact, "hr@parcelhub.dk"},
		{"other phone", withSentence("Call me on +45 98 76 54 32."), CheckContact, "+45 98 76 54 32"},
		{"square placeholder", withSentence("I admire [Company]."), CheckPlaceholder, `"["`},
		{"angle placeholder", withSentence("I admire <company name>."), CheckPlaceholder, `"<"`},
		{"curly placeholder", withSentence("I admire {company}."), CheckPlaceholder, `"{"`},
		{"template leftover", withSentence("I admire Your Name."), CheckPlaceholder, `"your name"`},
		{"exclamation", withSentence("I can start soon!"), CheckExclamation, "remove every exclamation mark"},
		{"exclamation in the subject", Letter{Subject: "Hire me!", Letter: validLetter().Letter}, CheckExclamation, "exclamation"},
		{"no salutation", Letter{Subject: testSubject, Letter: strings.TrimPrefix(validLetter().Letter, "Dear Hiring Manager,\n\n")}, CheckSalutation, "Dear Hiring Manager,"},
		{"salutation without comma", Letter{Subject: testSubject, Letter: strings.Replace(validLetter().Letter, "Dear Hiring Manager,", "Dear Hiring Manager", 1)}, CheckSalutation, "Dear"},
		{"no closing", Letter{Subject: testSubject, Letter: strings.Replace(validLetter().Letter, "Kind regards,\n\n", "", 1)}, CheckSignOff, "Kind regards,"},
		{"wrong name", Letter{Subject: testSubject, Letter: strings.Replace(validLetter().Letter, "\n\nJane Doe", "\n\nJohn Smith", 1)}, CheckSignOff, "full name"},
		{"no name", Letter{Subject: testSubject, Letter: strings.TrimSuffix(validLetter().Letter, "\n\nJane Doe")}, CheckSignOff, "full name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			violations := Check(tt.letter, testCandidate)
			require.NotEmpty(t, violations)
			var found *Violation
			for i := range violations {
				if violations[i].Check == tt.check {
					found = &violations[i]
				}
			}
			require.NotNil(t, found, "expected %s in %v", tt.check, violations)
			assert.Contains(t, found.Message, tt.message)
		})
	}
}

func TestCheck_AllowsWhatLooksLikeAViolationButIsNot(t *testing.T) {
	tests := map[string]string{
		"words containing profanity": "I assess risk in Scunthorpe and passed a class on shell scripting. Hello to the team.",
		"words containing a cliche":  "I passed the exam and compassionately handled support tickets.",
		"technologies with dots":     "I use ASP.NET, Node.js and Socket.io daily.",
		"own email":                  "You can reach me at JANE@example.dk.",
		"own phone, other format":    "You can reach me on 12345678.",
		"own phone with country":     "You can reach me on 0045 12 34 56 78.",
		"year ranges":                "I worked there from 2017-2021 and 2021 / 2024.",
		"numbers":                    "We process 30,000 events per minute and 2 TB a day for 120 services.",
		"named salutation":           "",
	}
	for name, sentence := range tests {
		t.Run(name, func(t *testing.T) {
			letter := withSentence(sentence)
			if name == "named salutation" {
				letter.Letter = strings.Replace(letter.Letter, "Dear Hiring Manager,", "Dear Ms. Jensen,", 1)
			}
			assert.Empty(t, Check(letter, testCandidate))
		})
	}
}

func TestCheck_OtherClosingsAndNoName(t *testing.T) {
	letter := validLetter()
	letter.Letter = strings.Replace(letter.Letter, "Kind regards,", "Yours sincerely,", 1)
	assert.Empty(t, Check(letter, testCandidate))
	assert.Empty(t, Check(letter, Candidate{}), "without a name any signature after a closing passes")

	letter.Letter = strings.Replace(letter.Letter, "Jane Doe", "jane   doe", 1)
	assert.Empty(t, Check(letter, testCandidate), "the name matches regardless of case and spacing")
}

func TestCheck_EmailIsNotAlsoAURL(t *testing.T) {
	violations := Check(withSentence("Write to hr@parcelhub.com."), testCandidate)
	assert.Equal(t, []string{CheckContact}, Names(violations))
}

func TestCheck_OwnContactsNeedAProfileValue(t *testing.T) {
	violations := Check(withSentence("Write to jane@example.dk."), Candidate{Name: "Jane Doe"})
	assert.Equal(t, []string{CheckContact}, Names(violations))
}

func TestWordCount(t *testing.T) {
	assert.Equal(t, 0, WordCount("  \n "))
	assert.Equal(t, 4, WordCount("Dear Hiring Manager,\n\nHello"))
}

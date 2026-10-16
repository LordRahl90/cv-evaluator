package entities

// CoverLetterBannedPhrases are clichés a cover letter must not use. The
// prompt lists them for the LLM and the cover letter checks reject a
// letter that uses any of them anyway, so the two stay in sync. Matching
// is case-insensitive on whole words.
var CoverLetterBannedPhrases = []string{
	"passionate",
	"passion",
	"perfect fit",
	"perfect match",
	"ideal candidate",
	"rockstar",
	"rock star",
	"ninja",
	"guru",
	"wizard",
	"dream job",
	"excited to",
	"thrilled",
	"world-class",
	"synergy",
	"go-getter",
	"think outside the box",
	"hit the ground running",
}

// Word limits of a cover letter. The prompt asks for the target range and
// the checks accept the wider allowed range, so a letter a few words off
// the target still passes.
const (
	CoverLetterTargetMinWords = 250
	CoverLetterTargetMaxWords = 350
	CoverLetterMinWords       = 200
	CoverLetterMaxWords       = 400
)

// CoverLetterInput is everything the cover letter prompt is rendered from.
// Every text field is untrusted (the job is pasted or scraped, the CV is
// uploaded and the evaluation comes from the client or an earlier LLM
// answer), so callers strip the prompt's tags from it first.
type CoverLetterInput struct {
	JobDescription string
	CV             string
	// Evaluation is the match evaluation rendered as text, or "" when
	// there is none.
	Evaluation string
	// CandidateName is the user's full name, which signs the letter.
	CandidateName string
	// Revision is set on a retry, after the previous answer failed the
	// checks.
	Revision *CoverLetterRevision
}

// CoverLetterRevision asks the LLM to fix its previous answer.
type CoverLetterRevision struct {
	// Previous is the LLM's previous raw answer.
	Previous string
	// Violations describe, one per item, why it was rejected.
	Violations []string
}

// BannedPhrases returns the clichés the prompt forbids.
func (in *CoverLetterInput) BannedPhrases() []string {
	return CoverLetterBannedPhrases
}

// TargetMinWords and TargetMaxWords are the word range the prompt asks for.
func (in *CoverLetterInput) TargetMinWords() int { return CoverLetterTargetMinWords }

// TargetMaxWords is the upper end of the word range the prompt asks for.
func (in *CoverLetterInput) TargetMaxWords() int { return CoverLetterTargetMaxWords }

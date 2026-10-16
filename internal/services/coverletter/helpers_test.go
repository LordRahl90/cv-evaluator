package coverletter

import (
	"encoding/json"
	"strings"
)

var testCandidate = Candidate{Name: "Jane Doe", Email: "jane@example.dk", Phone: "+45 12 34 56 78"}

const testSubject = "Application for Senior Backend Engineer"

// testParagraphs are the body of a letter that passes every check.
var testParagraphs = []string{
	"I am applying for the Senior Backend Engineer role at Parcelhub. For the past six years I have built and run Go services in production, " +
		"most recently at Parcelflow, where my team processes 30,000 shipment events per minute on Kafka and PostgreSQL. " +
		"Your posting puts distributed systems and Go at the centre of the role, and that is the work I have done every day since 2019.",
	"The posting asks for experience operating services at scale. At Parcelflow I introduced OpenTelemetry tracing across twelve services, " +
		"which cut our incident resolution time by 40 percent. I also led the move to event sourcing for the shipment ledger, " +
		"planning the migration so that no events were lost and the old and new models ran side by side for two months.",
	"You also ask for mentoring and technical leadership. I mentor three engineers, review most of the design documents in our group " +
		"and run the weekly architecture forum. Before Parcelflow I built REST and gRPC payment APIs in Go and Python at Ledgerly, " +
		"where I maintained the Docker based delivery pipeline on GitHub Actions for a team of eight.",
	"The role lists Kubernetes as a requirement. I have deployed my services with Docker on AWS ECS rather than Kubernetes, " +
		"and I have worked closely with the platform team that runs our clusters, so the concepts are familiar to me. " +
		"I would welcome the chance to discuss how my experience can support the work of your team. Thank you for considering my application.",
}

// testLetterBody joins the salutation, paragraphs and sign-off.
func testLetterBody(paragraphs ...string) string {
	parts := append([]string{"Dear Hiring Manager,"}, paragraphs...)
	parts = append(parts, "Kind regards,\n\nJane Doe")
	return strings.Join(parts, "\n\n")
}

func validLetter() Letter {
	body := testLetterBody(testParagraphs...)
	return Letter{Subject: testSubject, Letter: body, WordCount: WordCount(body)}
}

// rawAnswer is an LLM answer with the given subject and letter.
func rawAnswer(subject, letter string) string {
	b, err := json.Marshal(map[string]string{"subject": subject, "letter": letter})
	if err != nil {
		panic(err)
	}
	return string(b)
}

package main

import (
	"context"
	"cv-evaluator/db"
	"cv-evaluator/internal/migrator"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"cv-evaluator/internal/llm/deepseek"
	"cv-evaluator/internal/llm/ollama"
	"cv-evaluator/internal/services/matcher"

	deepseekLib "github.com/cohesion-org/deepseek-go"
	"github.com/joho/godotenv"
	"github.com/oklog/ulid/v2"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatal(err)
	}

	dbase, err := db.SetupDatabase()
	if err != nil {
		log.Fatal(err)
	}

	// migrate the database
	if err := migrator.Migrate(dbase); err != nil {
		log.Fatal(err)
	}

	// llm service
	deepseekKey := os.Getenv("DEEPSEEK_API_KEY")
	chatLLM := deepseek.New(deepseekLib.NewClient(deepseekKey))
	client := &http.Client{
		Timeout: 30 * time.Minute,
	}
	embeddingLLMService := ollama.New(client, os.Getenv("OLLAMA_BASE_URL"))

	// we process cv
	//processor:=cv.

	//b, err := os.ReadFile("./data/description.txt")
	b, err := os.ReadFile("./data/senior-software-engineer.txt")
	if err != nil {
		log.Fatal(err)
	}

	matchingService := matcher.New(dbase, embeddingLLMService, chatLLM)
	userID, err := parseUserID("0x019FBE666BCEC897F24FC84FE4323E2A")
	if err != nil {
		log.Fatal(err)
	}

	res, err := matchingService.MatchByJobDescription(context.TODO(), userID, string(b))
	if err != nil {
		log.Fatal(err)
	}

	bout, err := json.Marshal(res)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\n\n%s\n\n", bout)
}

func parseUserID(raw string) (ulid.ULID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ulid.ULID{}, fmt.Errorf("empty user id")
	}

	// Canonical ULID (26 chars) path.
	if id, err := ulid.ParseStrict(raw); err == nil {
		return id, nil
	}

	// Postgres bytea copy/paste forms: \x<32 hex> or 0x<32 hex>.
	hexPart := raw
	if strings.HasPrefix(hexPart, "\\x") || strings.HasPrefix(hexPart, "\\X") {
		hexPart = hexPart[2:]
	}
	if strings.HasPrefix(hexPart, "0x") || strings.HasPrefix(hexPart, "0X") {
		hexPart = hexPart[2:]
	}

	b, err := hex.DecodeString(hexPart)
	if err != nil {
		return ulid.ULID{}, fmt.Errorf("invalid ULID value %q: %w", raw, err)
	}
	if len(b) != 16 {
		return ulid.ULID{}, fmt.Errorf("invalid ULID byte length %d from %q", len(b), raw)
	}

	var id ulid.ULID
	copy(id[:], b)
	return id, nil
}

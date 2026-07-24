package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"cv-evaluator/db"
	"cv-evaluator/internal/llm/deepseek"
	"cv-evaluator/internal/llm/ollama"
	"cv-evaluator/internal/services/matcher"

	deepseekLib "github.com/cohesion-org/deepseek-go"
	"github.com/joho/godotenv"
	"github.com/oklog/ulid/v2"
)

var (
	userID string
	jobID  string
)

func main() {
	flag.StringVar(&userID, "user-id", "", "User ULID")
	flag.StringVar(&jobID, "job-id", "", "Job ID")
	flag.Parse()

	parsedUserID, err := ulid.ParseStrict(userID)
	if err != nil {
		log.Fatalf("invalid --user-id ULID: %v", err)
	}

	if err := godotenv.Load(); err != nil {
		log.Fatal(err)
	}

	dbase, err := db.SetupDatabase()
	if err != nil {
		log.Fatal(err)
	}

	// llm service
	deepseekKey := os.Getenv("DEEPSEEK_API_KEY")
	deepseekClient := deepseekLib.NewClient(deepseekKey)
	chatLLM := deepseek.New(deepseekClient)
	client := &http.Client{
		Timeout: 30 * time.Minute,
	}
	embeddingLLMService := ollama.New(client, os.Getenv("OLLAMA_BASE_URL"))

	matchService := matcher.New(dbase, embeddingLLMService, chatLLM)

	res, err := matchService.MatchByJobID(context.Background(), parsedUserID, jobID)
	if err != nil {
		log.Fatal(err)
	}

	b, err := json.Marshal(res)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\n\n%s\n\n", b)
}

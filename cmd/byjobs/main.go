package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"cv-evaluator/db"
	"cv-evaluator/internal/llm/deepseek"
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
	flag.StringVar(&jobID, "job-id", "", "Provider job ID (the job_id column); jobs are global, not per user")
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

	matchService := matcher.New(dbase, chatLLM)

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

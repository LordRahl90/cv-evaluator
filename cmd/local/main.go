package main

import (
	"context"
	"cv-evaluator/db"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
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

	// llm service
	deepseekKey := os.Getenv("DEEPSEEK_API_KEY")
	chatLLM := deepseek.New(deepseekLib.NewClient(deepseekKey))
	client := &http.Client{
		Timeout: 30 * time.Minute,
	}
	embeddingLLMService := ollama.New(client, os.Getenv("OLLAMA_BASE_URL"))

	// we process cv

	//b, err := os.ReadFile("./data/description.txt")
	b, err := os.ReadFile("./data/another-desc.txt")
	if err != nil {
		log.Fatal(err)
	}

	matchingService := matcher.New(dbase, embeddingLLMService, chatLLM)
	userID, err := ulid.ParseStrict("01ARZ3NDEKTSV4RRFFQ69G5FAV")
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

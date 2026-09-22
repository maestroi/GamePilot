package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/maestroi/GamePilot/emulator/ramclassify"
	"github.com/maestroi/GamePilot/emulator/ramdiscover"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gamepilot-classify:", err)
		os.Exit(1)
	}
}

func run() error {
	inPath := flag.String("in", "", "RAM discovery JSON path, or - for stdin")
	outPath := flag.String("out", "", "optional path to also write the generated schema JSON")
	baseURL := flag.String("base-url", envOr("OPENJEV_BASE_URL", "http://localhost:8000/v1"), "OpenJev/Jev-compatible base URL, normally ending in /v1")
	model := flag.String("model", envOr("OPENJEV_MODEL", "openjev-latest"), "OpenJev model name")
	apiKeyEnv := flag.String("api-key-env", "OPENJEV_API_KEY", "environment variable containing an optional OpenJev API key")
	minProbability := flag.Float64("min-probability", 0.55, "minimum selected-option probability for a scalar mapping")
	minConfidence := flag.Float64("min-confidence", 0.10, "minimum OpenJev confidence for a scalar mapping")
	maxAddresses := flag.Int("max-addresses", 48, "maximum ranked RAM addresses sent to the classifier")
	batchSize := flag.Int("batch-size", 16, "addresses classified per /v1/systemone request")
	timeout := flag.Duration("timeout", 60*time.Second, "HTTP timeout for each OpenJev request")
	flag.Parse()

	if *inPath == "" {
		return errors.New("-in is required")
	}
	if strings.TrimSpace(*baseURL) == "" {
		return errors.New("-base-url is required")
	}
	if strings.TrimSpace(*model) == "" {
		return errors.New("-model is required")
	}
	if *timeout <= 0 {
		return errors.New("-timeout must be positive")
	}

	report, err := readReport(*inPath)
	if err != nil {
		return err
	}
	apiKey := ""
	if *apiKeyEnv != "" {
		apiKey = os.Getenv(*apiKeyEnv)
	}
	client := ramclassify.NewOpenJevClient(*baseURL, apiKey)
	client.HTTPClient = &http.Client{Timeout: *timeout}
	cfg := ramclassify.Config{
		Model:          *model,
		MinProbability: *minProbability,
		MinConfidence:  *minConfidence,
		MaxAddresses:   *maxAddresses,
		BatchSize:      *batchSize,
	}
	schema, err := ramclassify.Classify(context.Background(), client, report, cfg)
	if err != nil {
		return err
	}
	payload, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return fmt.Errorf("encode schema: %w", err)
	}
	payload = append(payload, '\n')
	if _, err := os.Stdout.Write(payload); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}
	if *outPath != "" {
		if err := os.WriteFile(*outPath, payload, 0o644); err != nil {
			return fmt.Errorf("write schema %q: %w", *outPath, err)
		}
	}
	return nil
}

func readReport(path string) (ramdiscover.Report, error) {
	var reader io.Reader
	if path == "-" {
		reader = os.Stdin
	} else {
		file, err := os.Open(path)
		if err != nil {
			return ramdiscover.Report{}, fmt.Errorf("open discovery report %q: %w", path, err)
		}
		defer file.Close()
		reader = file
	}
	var report ramdiscover.Report
	decoder := json.NewDecoder(io.LimitReader(reader, 16<<20))
	if err := decoder.Decode(&report); err != nil {
		return ramdiscover.Report{}, fmt.Errorf("decode discovery report: %w", err)
	}
	return report, nil
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

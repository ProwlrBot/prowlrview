package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ProwlrBot/prowlrview/internal/caido"
	"github.com/ProwlrBot/prowlrview/internal/graph"
)

func runCaidoImport(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "caido-import: missing export.json")
		os.Exit(2)
	}
	file := args[0]
	g := graph.New()
	reqs, findings, err := caido.Import(file, g)
	if err != nil {
		die(err)
	}
	fmt.Printf("✓ imported %d requests, %d findings from %s\n", reqs, findings, file)
	fmt.Printf("  nodes in graph: %d\n", g.Len())
	for _, n := range g.Findings() {
		fmt.Printf("  [%s] %s\n", n.Severity, n.Label)
	}
}

func runCaidoPush(args []string) {
	endpoint := os.Getenv("CAIDO_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:8080"
	}
	var findingID string
	for i, a := range args {
		if a == "--endpoint" && i+1 < len(args) {
			endpoint = args[i+1]
		} else if a != "--endpoint" && (i == 0 || args[i-1] != "--endpoint") {
			findingID = a
		}
	}
	if findingID == "" {
		fmt.Fprintln(os.Stderr, "caido-push: missing finding-id")
		os.Exit(2)
	}
	matches, _ := filepath.Glob("*.snapshot.jsonl")
	if len(matches) == 0 {
		fmt.Fprintln(os.Stderr, "caido-push: no snapshot found; run prowlrview pipe/watch first")
		os.Exit(1)
	}
	g, err := graph.LoadFromPath(newestFile(matches))
	if err != nil {
		die(err)
	}
	if err := caido.Push(endpoint, findingID, g); err != nil {
		fmt.Fprintf(os.Stderr, "caido-push: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ pushed finding %s → %s\n", findingID, endpoint)
}

package main

import (
	"context"
	"flag"
	"fmt"

	"beishan/internal/bench"
)

func main() {
	suiteName := flag.String("suite", "smoke", "suite name")
	flag.Parse()

	handler := func(ctx context.Context, prompt string) (string, error) {
		return fmt.Sprintf("eval: received %q (len=%d)", prompt, len(prompt)), nil
	}

	switch *suiteName {
	case "smoke":
		fmt.Println(bench.RunAll(context.Background(), handler))
	default:
		fmt.Printf("unknown suite: %s\n", *suiteName)
	}
}

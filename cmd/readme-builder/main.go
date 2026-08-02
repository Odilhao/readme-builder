// Command readme-builder renders a file from public GitHub activity and RSS
// feeds. No credential is required.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/Odilhao/readme-builder/internal/config"
	"github.com/Odilhao/readme-builder/internal/fetch"
	"github.com/Odilhao/readme-builder/internal/render"
)

var (
	configPath     = flag.String("config", "", "Path to config.toml (required)")
	check          = flag.Bool("check", false, "Render and exit 1 on drift without writing")
	dumpJSON       = flag.String("dump-json", "", "Emit the data model as JSON to this path (- for stdout)")
	write          = flag.String("write", "", "Write rendered files (- for stdout)")
	verbose        = flag.Bool("v", false, "Verbose output")
	version        = flag.Bool("version", false, "Print version and exit")
	currentVersion = "dev"
)

func main() {
	flag.Parse()

	if *version {
		fmt.Printf("readme-builder version %s\n", currentVersion)
		os.Exit(0)
	}

	if *configPath == "" {
		fmt.Fprintf(os.Stderr, "error: -config is required\n")
		os.Exit(1)
	}

	// Check mutually exclusive flags
	if *check && *write != "" {
		fmt.Fprintf(os.Stderr, "error: -check and -write are mutually exclusive\n")
		os.Exit(1)
	}

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Create fetch client (token is optional; unauthenticated is valid)
	c, err := fetch.New(fetch.Options{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: fetch client: %v\n", err)
		os.Exit(1)
	}

	// Fetch data from configured sources
	data, err := fetch.Fetch(context.Background(), c, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: fetch: %v\n", err)
		os.Exit(1)
	}

	// Handle -dump-json
	if *dumpJSON != "" {
		jsonBytes, err := render.DumpJSON(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: dump JSON: %v\n", err)
			os.Exit(1)
		}

		if *dumpJSON == "-" {
			fmt.Print(string(jsonBytes))
		} else {
			if err := os.WriteFile(*dumpJSON, jsonBytes, 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "error: write JSON: %v\n", err)
				os.Exit(1)
			}
		}

		if *verbose {
			fmt.Fprintf(os.Stderr, "wrote data model to %s\n", *dumpJSON)
		}
		return
	}

	// Handle -check or -write
	hadDrift := false
	for _, r := range cfg.Render {
		if *check {
			drift, err := render.Check(data, r.Template, r.Output)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: check %s: %v\n", r.Output, err)
				os.Exit(1)
			}
			if drift {
				hadDrift = true
				if *verbose {
					fmt.Fprintf(os.Stderr, "drift detected: %s\n", r.Output)
				}
			}
		} else {
			// Default: render and write
			outPath := r.Output
			if *write != "" {
				outPath = *write
			}

			if err := render.Render(data, r.Template, outPath); err != nil {
				fmt.Fprintf(os.Stderr, "error: render %s: %v\n", r.Output, err)
				os.Exit(1)
			}
			if *verbose {
				fmt.Fprintf(os.Stderr, "wrote %s\n", outPath)
			}
		}
	}

	if hadDrift {
		os.Exit(1)
	}
}

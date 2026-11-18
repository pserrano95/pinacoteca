// Command e2e runs Pinacoteca browser journeys against a local test harness.
//
//	go run ./cmd/e2e                 # run all journeys
//	go run ./cmd/e2e view-artwork    # run one or more journeys by id
//	go run ./cmd/e2e --list          # list journeys and steps without running
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/pserrano95/pinacoteca/internal/e2e"
)

func main() {
	args := os.Args[1:]
	for _, a := range args {
		if a == "--list" {
			for _, j := range e2e.Journeys {
				vp := ""
				if j.Viewport == e2e.ViewportMobile {
					vp = " (mobile)"
				}
				fmt.Printf("%s%s: %s\n", j.ID, vp, j.What)
				for i, s := range j.Steps {
					fmt.Printf("  %d. %s\n", i+1, s.What)
				}
			}
			return
		}
	}

	var requested []string
	for _, a := range args {
		if !strings.HasPrefix(a, "--") {
			requested = append(requested, a)
		}
	}

	selected := e2e.Journeys
	if len(requested) > 0 {
		byID := make(map[string]e2e.Journey, len(e2e.Journeys))
		ids := make([]string, 0, len(e2e.Journeys))
		for _, j := range e2e.Journeys {
			byID[j.ID] = j
			ids = append(ids, j.ID)
		}
		selected = nil
		var unknown []string
		for _, id := range requested {
			j, ok := byID[id]
			if !ok {
				unknown = append(unknown, id)
				continue
			}
			selected = append(selected, j)
		}
		if len(unknown) > 0 {
			fmt.Fprintf(os.Stderr, "Unknown journey(s): %s. Available: %s\n", strings.Join(unknown, ", "), strings.Join(ids, ", "))
			os.Exit(1)
		}
	}

	if !e2e.RunAll(selected) {
		os.Exit(1)
	}
}

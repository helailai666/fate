package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/babyname/fate/v4"
	"github.com/babyname/fate/v4/config"
	fatehttp "github.com/babyname/fate/v4/internal/http"
	"github.com/babyname/fate/v4/resources"
)

func main() {
	addr := flag.String("addr", ":18080", "listen address")
	flag.Parse()

	cfg := config.DefaultConfig()
	f, err := fate.New(cfg)
	if err != nil {
		log.Fatal("failed to initialize fate: ", err)
	}

	handler := fatehttp.NewHandler(f, resources.StaticSub, f.Repo())

	fmt.Printf("Fate server listening on %s\n", *addr)
	fmt.Println("Endpoints:")
	fmt.Println("  GET  /health                - health check")
	fmt.Println("  POST /api/generate          - generate names")
	fmt.Println("  GET  /api/generate/status   - task status")
	fmt.Println("  GET  /api/generate/result   - task result")
	fmt.Println("  GET  /api/generate/names    - paged name list")
	fmt.Println("  GET  /api/generate/explore  - explore names")
	fmt.Println("  GET  /api/name-detail       - get name detail")
	fmt.Println("  POST /api/name-score        - score an existing name")
	fmt.Println("  GET  /api/poetry/search     - search poetry")
	fmt.Println("  GET  /                      - web UI (embedded)")

	if err := http.ListenAndServe(*addr, handler); err != nil {
		log.Fatal("server error: ", err)
	}
}

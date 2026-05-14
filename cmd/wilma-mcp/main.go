package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/jupe/wilma-mcp/internal/wilma"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	_ = godotenv.Load()

	transport := flag.String("transport", "stdio", "MCP transport to use: stdio or sse")
	listenAddr := flag.String("listen", ":8080", "Address for SSE mode")
	baseURL := flag.String("base-url", "http://localhost:8080", "Public base URL for SSE mode")
	basePath := flag.String("base-path", "/mcp", "SSE base path")
	flag.Parse()

	svc, err := wilma.NewServiceFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	mcpServer := wilma.NewMCPServer(svc)

	switch strings.ToLower(strings.TrimSpace(*transport)) {
	case "stdio":
		if err := server.ServeStdio(mcpServer); err != nil {
			log.Fatalf("stdio server error: %v", err)
		}
	case "sse":
		sseServer := server.NewSSEServer(
			mcpServer,
			server.WithBaseURL(*baseURL),
			server.WithBasePath(*basePath),
		)
		fmt.Fprintf(os.Stderr, "Starting SSE MCP server on %s (base %s%s)\n", *listenAddr, *baseURL, *basePath)
		if err := sseServer.Start(*listenAddr); err != nil {
			log.Fatalf("sse server error: %v", err)
		}
	default:
		log.Fatalf("invalid transport %q, expected stdio or sse", *transport)
	}
}

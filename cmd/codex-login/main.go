package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/YangKeao/haro-bot/internal/config"
	"github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/logging"
)

func main() {
	configPath := flag.String("config", "config.toml", "path to config file")
	flag.Parse()

	// Load config to get database DSN
	cfg, err := config.LoadFromFile(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logging
	if _, err := logging.Init(logging.Config{
		Level:       cfg.Log.Level,
		Development: cfg.Log.Development,
		Encoding:    cfg.Log.Encoding,
	}); err != nil {
		// Continue without structured logging
	}

	// Connect to database
	dbConn, err := db.Open(cfg.TiDBDSN)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to database: %v\n", err)
		os.Exit(1)
	}

	// Apply migrations to ensure oauth_tokens table exists
	if err := db.ApplyMigrations(dbConn, cfg.Memory); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to apply migrations: %v\n", err)
		os.Exit(1)
	}

	oauthConfig := llm.OAuthConfig{
		Enabled:     true,
		AutoRefresh: true,
	}

	manager := llm.NewCodexOAuthManager(oauthConfig, dbConn)

	if manager.IsAuthenticated() {
		fmt.Println("✅ Already authenticated!")
		fmt.Println("Run with -reauth flag to re-authenticate")
		return
	}

	ctx := context.Background()
	authURL, pkce, state, err := manager.GetAuthorizationURL()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to generate authorization URL: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n🔐 ChatGPT Codex OAuth Login")
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Println("1. Open this URL in your browser:")
	fmt.Println()
	fmt.Printf("   %s\n\n", authURL)
	fmt.Println("2. Sign in with your ChatGPT account")
	fmt.Println("3. After authorization, you'll be redirected to localhost")
	fmt.Println("4. Copy the 'code' parameter from the URL or paste the full redirect URL")
	fmt.Println()
	fmt.Print("Enter the code or redirect URL: ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	// Parse the input to extract code
	var code string
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		// Parse URL to extract code
		idx := strings.Index(input, "code=")
		if idx != -1 {
			codeStart := idx + 5
			codeEnd := strings.Index(input[codeStart:], "&")
			if codeEnd == -1 {
				codeEnd = strings.Index(input[codeStart:], "#")
			}
			if codeEnd == -1 {
				codeEnd = len(input) - codeStart
			}
			code = input[codeStart : codeStart+codeEnd]
		}
	} else {
		code = input
	}

	if code == "" {
		fmt.Fprintln(os.Stderr, "No authorization code provided")
		os.Exit(1)
	}

	// Check if state matches (optional for CLI)
	_ = state // We don't validate state in CLI mode for simplicity

	if err := manager.ExchangeCode(ctx, code, pkce.Verifier); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to exchange authorization code: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("✅ Successfully authenticated!")
	fmt.Println()
	fmt.Println("OAuth token has been stored in the database.")
	fmt.Println()
	fmt.Println("Add this to your config.toml:")
	fmt.Println()
	fmt.Println("[codex_oauth]")
	fmt.Println("enabled = true")
	fmt.Println("auto_refresh = true")
	fmt.Println("model = \"gpt-4o\"  # or gpt-4o-mini, gpt-4-turbo, etc.")
}

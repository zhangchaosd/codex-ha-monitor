package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"codex-monitor-agent/internal/discovery"
	"codex-monitor-agent/internal/hookrelay"
	"codex-monitor-agent/internal/httpapi"
	"codex-monitor-agent/internal/identity"
	"codex-monitor-agent/internal/model"
	"codex-monitor-agent/internal/monitor"
)

const version = "0.4.0"

func main() {
	if runHookCommand(os.Args[1:]) {
		return
	}
	var (
		port             = flag.Int("port", 8765, "HTTP listen port")
		bind             = flag.String("bind", "::", "HTTP bind address")
		token            = flag.String("token", "", "Required API bearer token")
		endpoint         = flag.String("codex", "auto", "Codex endpoint: auto or stdio")
		codexBinary      = flag.String("codex-bin", "codex", "Codex executable")
		codexHome        = flag.String("codex-home", "", "CODEX_HOME override")
		pollInterval     = flag.Duration("poll-interval", 10*time.Second, "App Server reconcile interval")
		requestTimeout   = flag.Duration("app-server-request-timeout", 10*time.Second, "Timeout for each App Server request")
		failureThreshold = flag.Int("app-server-failure-threshold", 2, "Consecutive account read failures before restarting App Server")
		usageHistoryDays = flag.Int("usage-history-days", 90, "Maximum daily usage buckets retained (0 disables buckets)")
		staleAfter       = flag.Duration("stale-after", 30*time.Second, "Snapshot stale threshold")
		activeWindow     = flag.Duration("filesystem-active-window", 60*time.Second, "Filesystem activity window")
		hookRunningTTL   = flag.Duration("hook-running-ttl", 10*time.Minute, "How long a working hook remains authoritative")
		hookIdleTTL      = flag.Duration("hook-idle-ttl", time.Minute, "How long an idle hook overrides filesystem activity")
		hookAttentionTTL = flag.Duration("hook-attention-ttl", 5*time.Minute, "How long approval/input hooks remain active")
		maxThreads       = flag.Int("max-threads", 100, "Maximum recent threads")
		mdns             = flag.Bool("mdns", true, "Advertise the agent with mDNS/Zeroconf")
		showVersion      = flag.Bool("version", false, "Print CMA version")
	)
	flag.Parse()
	if *showVersion {
		fmt.Printf("cma %s\n", version)
		return
	}
	if flag.NArg() > 0 {
		parsed, err := strconv.Atoi(flag.Arg(0))
		if err != nil {
			log.Fatalf("invalid port %q", flag.Arg(0))
		}
		*port = parsed
	}
	if *port < 1 || *port > 65535 {
		log.Fatalf("port must be between 1 and 65535")
	}
	if *requestTimeout <= 0 {
		log.Fatal("app-server-request-timeout must be greater than zero")
	}
	if *failureThreshold < 1 {
		log.Fatal("app-server-failure-threshold must be at least one")
	}
	if *usageHistoryDays < 0 || *usageHistoryDays > 365 {
		log.Fatal("usage-history-days must be between 0 and 365")
	}
	if *token == "" {
		log.Fatal("token is required")
	}
	installationID, err := identity.LoadOrCreate()
	if err != nil {
		log.Fatalf("load installation id: %v", err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	m := monitor.New(monitor.Config{
		AgentVersion: version, InstallationID: installationID,
		CodexBinary: *codexBinary, CodexHome: *codexHome, Endpoint: *endpoint,
		PollInterval: *pollInterval, FilesystemInterval: 2 * time.Second,
		AppServerRequestTimeout: *requestTimeout, AppServerFailureThreshold: *failureThreshold,
		UsageHistoryDays: *usageHistoryDays,
		StaleAfter:       *staleAfter, FilesystemActiveWindow: *activeWindow, MaxThreads: *maxThreads,
		HookRunningTTL: *hookRunningTTL, HookIdleTTL: *hookIdleTTL, HookAttentionTTL: *hookAttentionTTL,
	})
	go m.Run(ctx)
	address := net.JoinHostPort(*bind, strconv.Itoa(*port))
	if *mdns {
		if err := discovery.Advertise(ctx, *port, installationID, version, model.SchemaVersion); err != nil {
			log.Printf("mDNS discovery unavailable: %v", err)
		}
	}
	log.Printf("CMA %s listening on http://%s", version, address)
	if err := httpapi.New(address, m, *token).ListenAndServe(ctx); err != nil {
		log.Fatalf("HTTP server: %v", err)
	}
}

func runHookCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "hook-forward":
		token, endpoint, ok := hookArgs(args[1:])
		if !ok {
			log.Printf("hook-forward requires --token <token> [endpoint]")
			return true
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// Hooks must never hold up Codex. Forwarding is deliberately best effort
		// and emits no stdout, which is a valid successful hook response.
		_ = hookrelay.Forward(ctx, os.Stdin, endpoint, token)
		return true
	case "print-hook-config":
		token, endpoint, ok := hookArgs(args[1:])
		if !ok {
			log.Printf("print-hook-config requires --token <token> [endpoint]")
			return true
		}
		if err := hookrelay.PrintConfig(os.Stdout, hookrelay.Executable(), endpoint, token); err != nil {
			log.Printf("print hook config: %v", err)
		}
		return true
	default:
		return false
	}
}

func hookArgs(args []string) (token, endpoint string, ok bool) {
	endpoint = hookrelay.DefaultURL
	if len(args) < 2 || args[0] != "--token" || args[1] == "" {
		return "", "", false
	}
	token = args[1]
	if len(args) > 2 {
		endpoint = args[2]
	}
	return token, endpoint, true
}

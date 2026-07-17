package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"codex-monitor-agent/internal/hookrelay"
	"codex-monitor-agent/internal/httpapi"
	"codex-monitor-agent/internal/identity"
	"codex-monitor-agent/internal/monitor"
)

const version = "0.2.0"

func main() {
	if runHookCommand(os.Args[1:]) {
		return
	}
	var (
		port             = flag.Int("port", 8765, "HTTP listen port")
		bind             = flag.String("bind", "0.0.0.0", "HTTP bind address")
		endpoint         = flag.String("codex", "auto", "Codex endpoint: auto or stdio")
		codexBinary      = flag.String("codex-bin", "codex", "Codex executable")
		codexHome        = flag.String("codex-home", "", "CODEX_HOME override")
		pollInterval     = flag.Duration("poll-interval", 10*time.Second, "App Server reconcile interval")
		staleAfter       = flag.Duration("stale-after", 30*time.Second, "Snapshot stale threshold")
		activeWindow     = flag.Duration("filesystem-active-window", 60*time.Second, "Filesystem activity window")
		hookRunningTTL   = flag.Duration("hook-running-ttl", 10*time.Minute, "How long a working hook remains authoritative")
		hookIdleTTL      = flag.Duration("hook-idle-ttl", time.Minute, "How long an idle hook overrides filesystem activity")
		hookAttentionTTL = flag.Duration("hook-attention-ttl", 5*time.Minute, "How long approval/input hooks remain active")
		maxThreads       = flag.Int("max-threads", 100, "Maximum recent threads")
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
		StaleAfter: *staleAfter, FilesystemActiveWindow: *activeWindow, MaxThreads: *maxThreads,
		HookRunningTTL: *hookRunningTTL, HookIdleTTL: *hookIdleTTL, HookAttentionTTL: *hookAttentionTTL,
	})
	go m.Run(ctx)
	address := fmt.Sprintf("%s:%d", *bind, *port)
	log.Printf("CMA %s listening on http://%s", version, address)
	if err := httpapi.New(address, m).ListenAndServe(ctx); err != nil {
		log.Fatalf("HTTP server: %v", err)
	}
}

func runHookCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	endpoint := hookrelay.DefaultURL
	if len(args) >= 2 {
		endpoint = args[1]
	}
	switch args[0] {
	case "hook-forward":
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// Hooks must never hold up Codex. Forwarding is deliberately best effort
		// and emits no stdout, which is a valid successful hook response.
		_ = hookrelay.Forward(ctx, os.Stdin, endpoint)
		return true
	case "print-hook-config":
		if err := hookrelay.PrintConfig(os.Stdout, hookrelay.Executable(), endpoint); err != nil {
			log.Printf("print hook config: %v", err)
		}
		return true
	default:
		return false
	}
}

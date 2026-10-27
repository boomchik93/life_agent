package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // zone names in feeds must resolve even without system tzdata

	"lifeagent/internal/advice"
	"lifeagent/internal/api"
	"lifeagent/internal/calendar"
	"lifeagent/internal/config"
	"lifeagent/internal/llm"
	"lifeagent/internal/parse"
	"lifeagent/internal/store"
)

const backupKeep = 14 * 24 * time.Hour

func main() {
	envFile := flag.String("env", "", "optional KEY=VALUE file loaded for unset variables")
	flag.Parse()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := run(*envFile, flag.Arg(0), logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(envFile, cmd string, logger *slog.Logger) error {
	if envFile != "" {
		if err := config.LoadDotEnv(envFile); err != nil {
			return err
		}
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	switch cmd {
	case "", "serve":
		return serve(cfg, st, logger)
	case "eval":
		return runEval(cfg, st, flag.Args()[1:])
	case "backup":
		path, err := st.Backup(cfg.BackupDir, backupKeep, time.Now(), cfg.AgeRecipient)
		if err != nil {
			return err
		}
		fmt.Println(path)
		return nil
	default:
		return fmt.Errorf("unknown command %q (want serve, backup or eval)", cmd)
	}
}

// buildCalendars wires the configured calendar sources; it returns nil when none is set.
func buildCalendars(ctx context.Context, cfg config.Config, logger *slog.Logger) (*calendar.Aggregator, error) {
	agg := &calendar.Aggregator{}
	if len(cfg.CalendarIDs) > 0 {
		key, err := os.ReadFile(cfg.GoogleKeyFile)
		if err != nil {
			return nil, fmt.Errorf("read GOOGLE_SA_KEY_FILE: %w", err)
		}
		// One source per calendar: a failing calendar becomes a warning
		// instead of silently disappearing behind the others' events.
		for _, id := range cfg.CalendarIDs {
			g, err := calendar.NewGoogle(ctx, key, []string{id}, cfg.Location)
			if err != nil {
				return nil, err
			}
			agg.Sources = append(agg.Sources, calendar.Named{Name: "google", Provider: g})
		}
	}
	if cfg.ICSURL != "" {
		agg.Sources = append(agg.Sources, calendar.Named{Name: "ics", Provider: calendar.NewICS(cfg.ICSURL, "Расписание", cfg.Location)})
	}
	if len(agg.Sources) == 0 {
		logger.Warn("no calendar sources configured")
		return nil, nil
	}
	return agg, nil
}

// buildLLM wires the language-model stack; it returns nils when there is no API key.
func buildLLM(cfg config.Config, st *store.Store, logger *slog.Logger) (*llm.Client, *api.LLMInfo, error) {
	if cfg.OpenRouterKey == "" {
		logger.Warn("OPENROUTER_API_KEY is not set: phrase parsing is off")
		return nil, nil, nil
	}
	defaults, err := llm.DefaultConfig()
	if err != nil {
		return nil, nil, err
	}
	provider := llm.NewOpenAI(defaults.Provider.BaseURL, cfg.OpenRouterKey,
		time.Duration(defaults.Limits.TimeoutSec)*time.Second)
	catalog := llm.NewOpenRouterCatalog(defaults.Provider.BaseURL)
	client := &llm.Client{
		Provider: provider, ProviderName: defaults.Provider.Name,
		Quota: provider, Catalog: catalog, Journal: st,
		// Read on every call: a change made in the app applies without a restart.
		Config: func() (llm.Config, error) {
			kv, err := st.Settings()
			if err != nil {
				return llm.Config{}, err
			}
			return llm.Effective(defaults, kv)
		},
	}
	return client, &api.LLMInfo{Defaults: defaults, Catalog: catalog, Quota: provider}, nil
}

func serve(cfg config.Config, st *store.Store, logger *slog.Logger) error {
	var collector api.Collector
	agg, err := buildCalendars(context.Background(), cfg, logger)
	if err != nil {
		return err
	}
	if agg != nil {
		collector = agg
	}

	opts := api.Options{
		Store: st, Token: cfg.Token, Logger: logger,
		Calendar: collector, Location: cfg.Location,
		RatePerSec: 5, Burst: 20,
	}
	client, info, err := buildLLM(cfg, st, logger)
	if err != nil {
		return err
	}
	if cfg.CalendarWrite != "" {
		key, err := os.ReadFile(cfg.GoogleKeyFile)
		if err != nil {
			return fmt.Errorf("read GOOGLE_SA_KEY_FILE: %w", err)
		}
		w, err := calendar.NewGoogleWriter(context.Background(), key, cfg.CalendarWrite, cfg.Location)
		if err != nil {
			return err
		}
		opts.Writer = w
	} else {
		logger.Warn("CALENDAR_WRITE_ID is not set: writing events is off")
	}
	if client != nil {
		opts.Parser = &parse.Parser{LLM: client, Loc: cfg.Location, Now: time.Now}
		opts.Advisor = &advice.Advisor{LLM: client}
		opts.Caller = client
		opts.LLM = info
	}
	app := api.NewServer(opts)

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      100 * time.Second, // a parse may take up to four model calls
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go prepareAdviceLoop(ctx, app, logger)
	go pruneLoop(ctx, st, cfg.Location, logger)

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	logger.Info("listening", "addr", cfg.Listen)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func nopLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// adviceEvery is how often the server checks whether an automatic advice is due.
const adviceEvery = time.Hour

// prepareAdviceLoop makes the advice the phone will fetch. The rules and the
// quota check live in PrepareAdvice; a failure only waits for the next round.
func prepareAdviceLoop(ctx context.Context, app *api.Server, logger *slog.Logger) {
	t := time.NewTicker(adviceEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if a, err := app.PrepareAdvice(ctx); err != nil {
				logger.Error("prepare advice failed", "err", err)
			} else if a != nil {
				logger.Info("advice prepared", "id", a.ID)
			}
		}
	}
}

// activityKeep is how long daily sums are kept (PLAN.md section 14).
const activityKeep = 365 * 24 * time.Hour

// pruneLoop removes activity sums older than activityKeep once a day.
func pruneLoop(ctx context.Context, st *store.Store, loc *time.Location, logger *slog.Logger) {
	prune := func() {
		before := time.Now().In(loc).Add(-activityKeep).Format("2006-01-02")
		if n, err := st.PruneActivity(before); err != nil {
			logger.Error("prune activity failed", "err", err)
		} else if n > 0 {
			logger.Info("old activity pruned", "rows", n)
		}
	}
	prune()
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			prune()
		}
	}
}

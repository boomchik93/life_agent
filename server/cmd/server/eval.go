package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"lifeagent/internal/config"
	"lifeagent/internal/llm"
	"lifeagent/internal/parse"
	"lifeagent/internal/store"
)

const evalThreshold = 0.9 // PLAN.md section 14

var slugRe = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// runEval parses the reference phrases with one model and reports the share of
// correct cards. Progress is kept on disk: when the daily quota ends the run, the
// same command continues the next day. Calls are journaled with source "eval".
func runEval(cfg config.Config, st *store.Store, args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	setPath := fs.String("set", "eval/phrases.json", "reference phrases")
	outDir := fs.String("out", "data/eval", "directory for progress files")
	model := fs.String("model", "", "model to test (default: the configured parse model)")
	effort := fs.String("effort", "", "effort to test (default: the configured one)")
	fallbacks := fs.String("fallbacks", "", "comma-separated fallback models (default: none, so one model is measured)")
	pause := fs.Duration("pause", 4*time.Second, "pause between requests")
	limit := fs.Int("limit", 0, "at most this many new phrases in this run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if cfg.OpenRouterKey == "" {
		return fmt.Errorf("OPENROUTER_API_KEY is not set")
	}

	set, err := parse.LoadEvalSet(*setPath)
	if err != nil {
		return err
	}
	now, err := time.Parse(time.RFC3339, set.Now)
	if err != nil {
		return fmt.Errorf("eval set: now: %w", err)
	}
	now = now.In(cfg.Location)

	client, _, err := buildLLM(cfg, st, nopLogger())
	if err != nil {
		return err
	}
	base := client.Config
	client.Config = func() (llm.Config, error) {
		c, err := base()
		if err != nil {
			return c, err
		}
		tc := c.Tasks[llm.TaskParse]
		if *model != "" {
			tc.Model = *model
		}
		if *effort != "" {
			tc.Effort = *effort
		}
		tc.Fallbacks = nil
		if *fallbacks != "" {
			tc.Fallbacks = strings.Split(*fallbacks, ",")
		}
		c.Tasks[llm.TaskParse] = tc
		return c, nil
	}
	tc := mustTask(client.Config)
	if tc.Model == "" {
		return fmt.Errorf("no model: pass -model or choose one in the app")
	}

	if err := os.MkdirAll(*outDir, 0o700); err != nil {
		return err
	}
	// A new model, effort or prompt starts a new progress file.
	h := sha256.Sum256([]byte(parse.SystemPrompt(now) + string(parse.Schema) + tc.Effort + strings.Join(tc.Fallbacks, ",")))
	name := fmt.Sprintf("%s-%s-%s.jsonl", strings.Trim(slugRe.ReplaceAllString(tc.Model, "-"), "-"), tc.Effort, hex.EncodeToString(h[:4]))
	progress := filepath.Join(*outDir, name)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf("model %s, effort %s, %d phrases, progress %s\n", tc.Model, tc.Effort, len(set.Cases), progress)
	sum, err := parse.RunEval(ctx, parse.EvalOptions{
		Set: set, Progress: progress, Pause: *pause, Limit: *limit, Out: os.Stdout,
		Parser: &parse.Parser{LLM: client, Loc: cfg.Location, Now: func() time.Time { return now }},
	})
	for _, f := range sum.Failures {
		fmt.Printf("\n%s\n", f.Text)
		for _, d := range f.Diffs {
			fmt.Printf("    %s\n", d)
		}
	}
	fmt.Printf("\ndone %d of %d, correct %d (%.0f%%), category guessed %d\n",
		sum.Done, sum.Total, sum.Correct, 100*sum.Accuracy(), sum.CategoryOK)
	if sum.Skipped > 0 {
		fmt.Printf("%d phrases skipped because the model was unavailable; run the same command again to retry them\n", sum.Skipped)
	}
	if sum.ResetAt != nil {
		fmt.Printf("daily limit reached; run the same command again after %s\n", sum.ResetAt.In(cfg.Location).Format("2006-01-02 15:04 MST"))
	}
	if err != nil {
		return err
	}
	if sum.Done == sum.Total {
		verdict := "BELOW"
		if sum.Accuracy() >= evalThreshold {
			verdict = "meets"
		}
		fmt.Printf("%s the %.0f%% threshold\n", verdict, 100*evalThreshold)
	}
	return nil
}

func mustTask(c func() (llm.Config, error)) llm.TaskConfig {
	cfg, err := c()
	if err != nil {
		return llm.TaskConfig{}
	}
	return cfg.Tasks[llm.TaskParse]
}

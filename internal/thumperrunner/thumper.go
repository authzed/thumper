package thumperrunner

import (
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/authzed/authzed-go/v1"
	"github.com/mroth/weightedrand"
	"github.com/rs/zerolog/log"
)

// WorkerOptions represent the configuration for the worker
type WorkerOptions struct {
	Index             int
	Client            *authzed.Client
	Scripts           []*ExecutableScript
	StepTimeout       time.Duration
	StepInterval      time.Duration
	StepRandomization bool
}

// RunWorker runs a worker, with the given index and set of executable Scripts.
func RunWorker(options WorkerOptions) {
	choices := make([]weightedrand.Choice, 0, len(options.Scripts))
	for _, script := range options.Scripts {
		numExecuted := 0
		if options.StepRandomization {
			numExecuted = rand.Intn(len(script.steps))
		}
		executable := &ExecutableContext{
			script:      script,
			client:      options.Client,
			numExecuted: numExecuted,
			source:      script.source,
		}
		choices = append(choices, weightedrand.NewChoice(executable, script.weight))
	}

	// Pre-process the configuration options into a chooser
	chooser, err := weightedrand.NewChooser(choices...)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to create weighted random chooser")
	}

	log.Info().Int("worker", options.Index).Msg("starting worker")

	stepInterval := options.StepInterval
	if stepInterval <= 0 {
		stepInterval = 1 * time.Second
	}
	ticker := time.NewTicker(stepInterval)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	for {
		select {
		case <-sigs:
			log.Info().Int("worker", options.Index).Msg("stopping worker")
			return
		case <-ticker.C:
			chosen := chooser.Pick().(*ExecutableContext)
			// Execute the step synchronously: a worker walks its script's steps
			// one at a time. Launching this in a goroutine let a slow step's
			// successor start before numExecuted was advanced (it is incremented
			// only after the RPC returns), so overlapping goroutines re-ran the
			// SAME step with identical template values — duplicate concurrent
			// writes of the same relationship, which CockroachDB rejects with
			// WriteTooOldError (SQLSTATE 40001). It also raced on the shared
			// numExecuted/zedToken/script fields. Concurrency is provided by
			// running multiple workers (--qps), not by overlapping a worker with
			// itself. If a step outlasts the interval, time.Ticker drops the
			// missed ticks, which is honest backpressure for a load test.
			chosen.StepForward(options.Index, options.StepTimeout)
		}
	}
}

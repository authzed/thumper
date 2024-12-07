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
	StepRandomization bool
}

// RunWorker runs a worker, with the given index and set of executable Scripts.
func RunWorker(options WorkerOptions) {
	choices := make([]weightedrand.Choice, 0)
	for _, script := range options.Scripts {
		numExecuted := 0
		if options.StepRandomization {
			numExecuted = rand.Intn(len(script.steps))
		}
		executable := &ExecutableContext{
			script:      script,
			client:      options.Client,
			numExecuted: numExecuted,
		}
		choices = append(choices, weightedrand.NewChoice(executable, script.weight))
	}

	// Pre-process the configuration options into a chooser
	chooser, err := weightedrand.NewChooser(choices...)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to create weighted random chooser")
	}

	log.Info().Int("worker", options.Index).Msg("starting worker")

	ticker := time.NewTicker(1 * time.Second)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	for {
		select {
		case <-sigs:
			log.Info().Int("worker", options.Index).Msg("stopping worker")
			return
		case <-ticker.C:
			chosen := chooser.Pick().(*ExecutableContext)
			go chosen.StepForward(options.Index, options.StepTimeout)
		}
	}
}

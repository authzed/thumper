package thumperrunner

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	"github.com/authzed/authzed-go/v1"
	"github.com/rs/zerolog/log"
)

type executableStep struct {
	op          string
	consistency string
	body        func(context.Context, *authzed.Client, *v1.ZedToken) (*v1.ZedToken, error)
}

// ExecutableScript is a thumper yaml script that has been post-processed for
// execution efficiency.
type ExecutableScript struct {
	name   string
	weight uint
	steps  []executableStep
}

type ExecutableContext struct {
	script *ExecutableScript
	client *authzed.Client

	numExecuted int
	zedToken    *v1.ZedToken
}

// StepForward advances the script one step and then stops.
func (s *ExecutableContext) StepForward(workerIndex int, stepTimeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), stepTimeout)
	defer cancel()

	stepNum := s.numExecuted % len(s.script.steps)
	step := s.script.steps[stepNum]

	log.Debug().
		Str("script", s.script.name).
		Int("step", stepNum).
		Int("worker", workerIndex).
		Str("op", step.op).
		Str("consistency", step.consistency).
		Msg("executing script step")

	newToken, err := step.body(ctx, s.client, s.zedToken)
	if err != nil {
		log.Warn().
			Str("script", s.script.name).
			Int("step", stepNum).
			Int("worker", workerIndex).
			Str("op", step.op).
			Str("consistency", step.consistency).
			Err(err).
			Msg("error calling script step")
	}

	s.numExecuted++
	s.zedToken = newToken
}

// RunOnce runs all steps in a script and then stops.
func (s *ExecutableScript) RunOnce(client *authzed.Client) error {
	log.Info().Str("script", s.name).Msg("running migration script")

	ctx, cancel := context.WithTimeout(context.Background(), 3600*time.Second)
	defer cancel()

	for stepNum, step := range s.steps {
		log.Debug().Int("step", stepNum).Int("total", len(s.steps)).Msg("executing migration step")
		_, err := step.body(ctx, client, nil)
		if err != nil {
			return fmt.Errorf("error running script %s: %w", s.name, err)
		}
	}

	return nil
}

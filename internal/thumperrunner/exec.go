package thumperrunner

import (
	"context"
	"fmt"
	"time"

	"github.com/authzed/internal/thumper/internal/config"

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

	// source, when set, allows the script to be re-rendered from its origin
	// file at the start of each execution cycle (see EnableRerender).
	source *scriptSource
}

type ExecutableContext struct {
	script *ExecutableScript
	client *authzed.Client

	numExecuted int
	zedToken    *v1.ZedToken

	// source is copied from the script when the context is created; when
	// non-nil the script is re-rendered at the start of each cycle.
	source *scriptSource
}

// scriptSource records where a script was loaded from so it can be re-rendered
// (re-templated) on demand, regenerating template values such as randomObjectID.
type scriptSource struct {
	filename string
	vars     config.ScriptVariables
	docIndex int
}

// load re-renders the source file and returns a freshly prepared copy of the
// document at docIndex.
func (src *scriptSource) load() (*ExecutableScript, error) {
	scripts, _, err := config.Load(src.filename, src.vars)
	if err != nil {
		return nil, fmt.Errorf("error re-loading script %s: %w", src.filename, err)
	}

	prepared, err := Prepare(scripts)
	if err != nil {
		return nil, fmt.Errorf("error re-preparing script %s: %w", src.filename, err)
	}

	if src.docIndex >= len(prepared) {
		return nil, fmt.Errorf("script %s no longer contains document %d", src.filename, src.docIndex)
	}

	return prepared[src.docIndex], nil
}

// EnableRerender attaches a re-render source to each prepared script so that,
// at the start of every cycle through its steps, the script is re-rendered from
// filename. This regenerates template values (notably randomObjectID) for each
// cycle instead of baking them in once at load time.
func EnableRerender(scripts []*ExecutableScript, filename string, vars config.ScriptVariables) {
	for idx, s := range scripts {
		s.source = &scriptSource{filename: filename, vars: vars, docIndex: idx}
	}
}

// StepForward advances the script one step and then stops.
func (s *ExecutableContext) StepForward(workerIndex int, stepTimeout time.Duration) {
	// At the start of each cycle through the script's steps, re-render the
	// script from its source (if enabled) so template values such as
	// randomObjectID are regenerated for this cycle.
	if s.source != nil && s.numExecuted%len(s.script.steps) == 0 {
		if fresh, err := s.source.load(); err != nil {
			log.Warn().
				Err(err).
				Str("script", s.script.name).
				Int("worker", workerIndex).
				Msg("failed to re-render script; reusing previous render")
		} else {
			s.script = fresh
		}
	}

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

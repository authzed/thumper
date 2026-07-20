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

	// source, when set, holds the parsed template so the script's random IDs
	// can be regenerated at the start of each execution cycle (see
	// PrepareRenderable).
	source *scriptSource
}

type ExecutableContext struct {
	script *ExecutableScript
	client *authzed.Client

	numExecuted int
	zedToken    *v1.ZedToken

	// source is copied from the script when the context is created; when
	// non-nil the script is re-rendered (its random IDs regenerated) at the
	// start of each cycle through its steps.
	source *scriptSource
}

// scriptSource holds the once-parsed script template (with random-ID
// placeholders intact) so the script can be cheaply re-rendered on demand:
// resolving the placeholders to fresh random values, with no re-parsing.
type scriptSource struct {
	template *config.Script
}

// load resolves the template's random-ID placeholders to fresh values and
// prepares the result for execution. It performs no file I/O, template
// execution, or YAML parsing — only an in-memory resolve plus proto build.
func (src *scriptSource) load() (*ExecutableScript, error) {
	// Prepare returns exactly one ExecutableScript per input script.
	prepared, err := Prepare([]*config.Script{config.ResolveScript(src.template)})
	if err != nil {
		return nil, fmt.Errorf("error re-rendering script %s: %w", src.template.Name, err)
	}
	return prepared[0], nil
}

// PrepareRenderable prepares scripts that use random IDs. Each returned script
// is an initial render (its placeholders resolved to fresh values) and carries
// its parsed template so it can be re-rendered with new random values at each
// cycle boundary (see StepForward). Give each worker its own PrepareRenderable
// result so workers generate independent data.
func PrepareRenderable(templates []*config.Script) ([]*ExecutableScript, error) {
	prepared := make([]*ExecutableScript, 0, len(templates))
	for _, template := range templates {
		src := &scriptSource{template: template}
		script, err := src.load()
		if err != nil {
			return nil, err
		}
		script.source = src
		prepared = append(prepared, script)
	}
	return prepared, nil
}

// StepForward advances the script one step and then stops.
func (s *ExecutableContext) StepForward(workerIndex int, stepTimeout time.Duration) {
	// At the start of each cycle through the script's steps, re-render the
	// script from its source (if it uses random IDs) so those IDs are
	// regenerated for the new cycle. This resolves placeholders in memory — no
	// file, template, or YAML re-parsing — so it is cheap enough to do every
	// cycle.
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

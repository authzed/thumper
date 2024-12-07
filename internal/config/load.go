package config

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"math/rand"
	"path"

	"github.com/Masterminds/sprig/v3"
	"github.com/ccoveille/go-safecast"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

// Load reads a script file, replaces the templated values with the values from
// the execution environment, and then processes it as a thumper script yaml.
func Load(filename string, vars ScriptVariables) ([]*Script, bool, error) {
	usedRandom := false
	randomID := randomObjectID(64)
	tmpl := template.New(path.Base(filename)).Funcs(template.FuncMap{
		"enumerate": func(count uint) []uint {
			indices := make([]uint, count)
			for i := range indices {
				// NOTE: This is technically safe because range
				// is always nonnegative, but gosec doesn't know
				// that yet.
				// TODO: remove this when gosec catches up
				index, _ := safecast.ToUint(i)
				indices[i] = index
			}
			return indices
		},
		"randomObjectID": func() string {
			usedRandom = true
			return randomID
		},
	}).Funcs(sprig.FuncMap())

	parsed, err := tmpl.ParseFiles(filename)
	if err != nil {
		return nil, false, fmt.Errorf("error parsing script %s: %w", filename, err)
	}

	buf := &bytes.Buffer{}
	if err := parsed.Execute(buf, vars); err != nil {
		return nil, false, fmt.Errorf("error rendering config: %w", err)
	}

	dec := yaml.NewDecoder(buf)

	var scripts []*Script
	for {
		var script Script
		err := dec.Decode(&script)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, false, fmt.Errorf("unable to decode yaml: %w", err)
		}

		log.Info().Str("name", script.Name).Msg("loaded script")

		scripts = append(scripts, &script)
	}

	return scripts, usedRandom, nil
}

const (
	firstLetters      = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890_"
	subsequentLetters = firstLetters + "/_|-"
)

func randomObjectID(length uint8) string {
	b := make([]byte, length)
	for i := range b {
		sourceLetters := subsequentLetters
		if i == 0 {
			sourceLetters = firstLetters
		}
		b[i] = sourceLetters[rand.Intn(len(sourceLetters))]
	}
	return string(b)
}

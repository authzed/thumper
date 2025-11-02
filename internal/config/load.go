package config

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"math/rand"
	"os"
	"path"

	"github.com/Masterminds/sprig/v3"
	"github.com/ccoveille/go-safecast"
	"github.com/goccy/go-yaml"
	"github.com/rs/zerolog/log"
)

// Load reads a script file, replaces the templated values with the values from
// the execution environment, and then processes it as a thumper script yaml.
func Load(filename string, vars ScriptVariables) ([]*Script, bool, error) {
	usedRandom := false
	randomID := randomObjectID(64)

	// Look for the file in the given path *or* in the kodata dir
	filepath, err := findFile(filename, os.Getenv("KO_DATA_PATH"))
	if err != nil {
		return nil, false, err
	}

	tmpl := template.New(path.Base(filepath)).Funcs(template.FuncMap{
		"enumerate": func(count uint) []uint {
			indices := make([]uint, count)
			for i := range indices {
				// NOTE: This is technically safe because range
				// is always nonnegative, but gosec doesn't know
				// that yet.
				// TODO: remove this when gosec catches up
				index, _ := safecast.Convert[uint](i)
				indices[i] = index
			}
			return indices
		},
		"randomObjectID": func() string {
			usedRandom = true
			return randomID
		},
	}).Funcs(sprig.FuncMap())

	parsed, err := tmpl.ParseFiles(filepath)
	if err != nil {
		return nil, false, fmt.Errorf("error parsing script %s: %w", filepath, err)
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

// A pre-loaded file from the scripts dir is going to be put in the kodata
// directory, which we don't want our users to have to find.
// We first look for the file at the given path, then at
// the kodata location, and return the found path or else error.
func findFile(filename, koPath string) (string, error) {
	_, firstErr := os.Stat(filename)
	if firstErr == nil {
		return filename, nil
	}

	koFilepath := path.Join(koPath, filename)

	_, fallbackErr := os.Stat(koFilepath)
	if fallbackErr == nil {
		return koFilepath, nil
	}

	// If we can't find the file, we return the first error, because the
	// kodata lookup is an implementation detail.
	return "", firstErr
}

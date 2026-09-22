package config

import (
	"strings"
	"unicode/utf8"
)

// Random-ID placeholders are delimited by Unicode private-use-area runes: they
// never appear in real object IDs (so substitution can't hit a real value) and
// survive YAML decoding untouched, so a placeholder embedded in an ID
// (e.g. "org_<ph>") arrives intact at ResolveScript.
const (
	randomIDStart = '\ue000'
	randomIDEnd   = '\ue001'
)

// randomIDPlaceholder returns the placeholder token for a given key. See the
// randomObjectID template function in Load.
func randomIDPlaceholder(key string) string {
	return string(randomIDStart) + key + string(randomIDEnd)
}

// substituteRandomIDs replaces every random-ID placeholder in s with gen(key),
// where key is the text the placeholder was created with. gen is expected to
// return the same value for repeated calls with the same key so that IDs
// referenced in multiple places stay consistent within one resolution.
func substituteRandomIDs(s string, gen func(key string) string) string {
	i := strings.IndexRune(s, randomIDStart)
	if i < 0 {
		return s
	}

	var b strings.Builder
	b.Grow(len(s)) // resolved values are at least as long as the tokens they replace
	for {
		b.WriteString(s[:i])

		rest := s[i+utf8.RuneLen(randomIDStart):]
		j := strings.IndexRune(rest, randomIDEnd)
		if j < 0 {
			// Unterminated placeholder; emit the remainder verbatim rather than
			// dropping data.
			b.WriteString(s[i:])
			return b.String()
		}

		b.WriteString(gen(rest[:j]))
		s = rest[j+utf8.RuneLen(randomIDEnd):]

		i = strings.IndexRune(s, randomIDStart)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
	}
}

// ResolveScript returns a copy of template with every random-ID placeholder
// replaced by a freshly generated random object ID. Placeholders that share a
// key resolve to the same value within the returned script (so, for example, a
// CREATE and a DELETE step that reference the same key operate on the same
// relationship), while distinct keys resolve to independent, uniformly
// distributed values (scattering writes across the keyspace). Each call
// generates new values, so calling it once per cycle yields fresh data without
// re-parsing the script.
//
// The input template is not modified and may be resolved concurrently.
func ResolveScript(template *Script) *Script {
	values := make(map[string]string)
	gen := func(key string) string {
		v, ok := values[key]
		if !ok {
			v = randomObjectID(64)
			values[key] = v
		}
		return v
	}
	return resolveScript(template, gen)
}

func resolveScript(in *Script, gen func(string) string) *Script {
	out := &Script{
		Name:      in.Name,
		Weight:    in.Weight,
		RecordTTL: in.RecordTTL,
	}
	if len(in.Steps) > 0 {
		out.Steps = make([]ScriptStep, len(in.Steps))
		for i, step := range in.Steps {
			out.Steps[i] = resolveStep(step, gen)
		}
	}
	return out
}

// resolveStep receives step by value (a shallow copy) and returns it with every
// placeholder-bearing string field resolved. Slices that are rewritten are
// reallocated so the input script's backing arrays are never mutated.
func resolveStep(step ScriptStep, gen func(string) string) ScriptStep {
	sub := func(p *string) { *p = substituteRandomIDs(*p, gen) }

	// Only the fields that carry object references / IDs are resolved; keyword
	// fields such as Op and Consistency are left alone. Context (*ProtoStruct)
	// is intentionally not descended into.
	sub(&step.Resource)
	sub(&step.Subject)
	sub(&step.Permission)
	sub(&step.Schema)

	if len(step.Updates) > 0 {
		updates := make([]Update, len(step.Updates))
		for i, u := range step.Updates {
			sub(&u.Resource)
			sub(&u.Subject)
			sub(&u.Relation)
			if u.Caveat != nil {
				caveat := *u.Caveat
				sub(&caveat.Name)
				u.Caveat = &caveat
			}
			updates[i] = u
		}
		step.Updates = updates
	}

	if len(step.Checks) > 0 {
		checks := make([]Check, len(step.Checks))
		for i, c := range step.Checks {
			sub(&c.Resource)
			sub(&c.Subject)
			sub(&c.Permission)
			checks[i] = c
		}
		step.Checks = checks
	}

	return step
}

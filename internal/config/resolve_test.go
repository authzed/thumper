package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubstituteRandomIDs(t *testing.T) {
	gen := func(key string) string { return "<" + key + ">" }

	t.Run("no placeholder is unchanged", func(t *testing.T) {
		require.Equal(t, "organization:org_abc", substituteRandomIDs("organization:org_abc", gen))
	})

	t.Run("single placeholder is replaced", func(t *testing.T) {
		in := "org_" + randomIDPlaceholder("7")
		require.Equal(t, "org_<7>", substituteRandomIDs(in, gen))
	})

	t.Run("multiple placeholders in one string", func(t *testing.T) {
		in := randomIDPlaceholder("a") + "_" + randomIDPlaceholder("b")
		require.Equal(t, "<a>_<b>", substituteRandomIDs(in, gen))
	})

	t.Run("keyless placeholder uses the empty key", func(t *testing.T) {
		require.Equal(t, "<>", substituteRandomIDs(randomIDPlaceholder(""), gen))
	})
}

func TestResolveScriptMemoizesPerKey(t *testing.T) {
	// Same key -> same value; different keys -> (almost surely) different values.
	template := &Script{Steps: []ScriptStep{{
		Op:       "WriteRelationships",
		Resource: "res_" + randomIDPlaceholder("0"),
		Subject:  "sub_" + randomIDPlaceholder("0"),
		Updates: []Update{{
			Resource: "u_" + randomIDPlaceholder("1"),
		}},
	}}}

	resolved := ResolveScript(template)
	step := resolved.Steps[0]

	resID := strings.TrimPrefix(step.Resource, "res_")
	subID := strings.TrimPrefix(step.Subject, "sub_")
	updID := strings.TrimPrefix(step.Updates[0].Resource, "u_")

	require.NotContains(t, step.Resource, string(randomIDStart), "placeholder should be fully resolved")
	require.Equal(t, resID, subID, "same key must resolve to the same value within a script")
	require.NotEqual(t, resID, updID, "different keys must resolve to different values")

	// The input template must not be mutated (safe for concurrent reuse).
	require.Contains(t, template.Steps[0].Resource, string(randomIDStart), "template must be left untouched")
}

// TestAddDeleteScriptRoundTrip exercises the whole pipeline against the real
// script: the placeholders must survive Load's template + YAML decoding, resolve
// cleanly, correlate the CREATE and DELETE steps, and be fresh on every resolve.
func TestAddDeleteScriptRoundTrip(t *testing.T) {
	const orgPrefix = "thumper/organization:org_"

	scripts, usedRandom, err := Load("../../scripts/add-delete-data.yaml", ScriptVariables{Prefix: "thumper/"})
	require.NoError(t, err)
	require.True(t, usedRandom, "script uses randomObjectID")
	require.Len(t, scripts, 1)

	template := scripts[0]
	require.Len(t, template.Steps, 2, "expect one CREATE step and one DELETE step")

	// orgID returns the org object id used by a resolved step's first update.
	orgID := func(s ScriptStep) string {
		require.NotEmpty(t, s.Updates)
		res := s.Updates[0].Resource
		require.NotContains(t, res, string(randomIDStart), "no placeholder should survive resolution")
		require.True(t, strings.HasPrefix(res, orgPrefix), "unexpected resource: %s", res)
		return strings.TrimPrefix(res, orgPrefix)
	}

	resolved := ResolveScript(template)

	// Steps are [create, delete]; both must reference the same relationship.
	create, del := orgID(resolved.Steps[0]), orgID(resolved.Steps[1])
	require.Equal(t, create, del, "the CREATE and DELETE steps must reference the same id")

	// Re-resolving yields fresh values.
	again := ResolveScript(template)
	require.NotEqual(t, create, orgID(again.Steps[0]), "each resolve must produce fresh ids")
}

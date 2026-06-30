package config

import (
	"testing"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestScriptRecordTTL(t *testing.T) {
	testCases := []struct {
		name     string
		yaml     string
		expected time.Duration
	}{
		{
			name:     "absent",
			yaml:     "name: s\nsteps: []\n",
			expected: 0,
		},
		{
			name:     "seconds",
			yaml:     "name: s\nrecordTtl: 30s\nsteps: []\n",
			expected: 30 * time.Second,
		},
		{
			name:     "minutes",
			yaml:     "name: s\nrecordTtl: 5m\nsteps: []\n",
			expected: 5 * time.Minute,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var script Script
			require.NoError(t, yaml.Unmarshal([]byte(tc.yaml), &script))
			require.Equal(t, tc.expected, script.RecordTTL)
		})
	}
}

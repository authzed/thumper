package config

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStats(t *testing.T) {
	testCases := []struct {
		scripts  []*Script
		expected map[string]float32
	}{
		{
			[]*Script{
				{
					Weight: 1,
					Steps:  []ScriptStep{{Op: "write"}},
				},
				{
					Weight: 1,
					Steps:  []ScriptStep{{Op: "write"}},
				},
			},
			map[string]float32{
				"write": 1.0,
			},
		},
		{
			[]*Script{
				{
					Weight: 0,
					Steps:  []ScriptStep{{Op: "write"}},
				},
				{
					Weight: 1,
					Steps:  []ScriptStep{{Op: "check"}},
				},
			},
			map[string]float32{
				"check": 1.0,
				"write": 0.0,
			},
		},
		{
			[]*Script{
				{
					Weight: 1,
					Steps:  []ScriptStep{{Op: "write"}},
				},
				{
					Weight: 1,
					Steps:  []ScriptStep{{Op: "check"}},
				},
			},
			map[string]float32{
				"check": 0.5,
				"write": 0.5,
			},
		},
		{
			[]*Script{
				{
					Weight: 1,
					Steps:  []ScriptStep{{Op: "write"}},
				},
				{
					Weight: 1,
					Steps:  []ScriptStep{{Op: "check"}, {Op: "check"}},
				},
			},
			map[string]float32{
				"check": 0.5,
				"write": 0.5,
			},
		},
		{
			[]*Script{
				{
					Weight: 1,
					Steps:  []ScriptStep{{Op: "write"}},
				},
				{
					Weight: 1,
					Steps:  []ScriptStep{{Op: "check"}, {Op: "write"}},
				},
			},
			map[string]float32{
				"check": 0.25,
				"write": 0.75,
			},
		},
		{
			[]*Script{
				{
					Weight: 99,
					Steps:  []ScriptStep{{Op: "write"}},
				},
				{
					Weight: 1,
					Steps:  []ScriptStep{{Op: "check"}},
				},
			},
			map[string]float32{
				"check": 0.01,
				"write": 0.99,
			},
		},
		{
			[]*Script{
				{
					Weight: 99,
					Steps:  []ScriptStep{{Op: "write"}},
				},
				{
					Weight: 1,
					Steps:  []ScriptStep{{Op: "check"}, {Op: "check"}},
				},
			},
			map[string]float32{
				"check": 0.01,
				"write": 0.99,
			},
		},
		{
			[]*Script{
				{
					Weight: 99,
					Steps:  []ScriptStep{{Op: "write"}},
				},
				{
					Weight: 1,
					Steps:  []ScriptStep{{Op: "check"}, {Op: "write"}},
				},
			},
			map[string]float32{
				"check": 0.005,
				"write": 0.995,
			},
		},
	}

	for i, tc := range testCases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			actual := Stats(tc.scripts)
			require.InDeltaMapValues(t, tc.expected, actual, .001)
		})
	}
}

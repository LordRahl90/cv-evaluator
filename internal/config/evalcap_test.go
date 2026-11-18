package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_EvalDailyCap(t *testing.T) {
	for value, want := range map[string]int{"": DefaultEvalDailyCap, "0": 0, "12": 12} {
		setValidEnv(t)
		t.Setenv("WORKER_EVAL_DAILY_CAP", value)
		cfg, err := Load()
		require.NoError(t, err, value)
		assert.Equal(t, want, cfg.EvalDailyCap, value)
	}

	for _, value := range []string{"-1", "lots"} {
		setValidEnv(t)
		t.Setenv("WORKER_EVAL_DAILY_CAP", value)
		_, err := Load()
		require.Error(t, err, value)
		assert.Contains(t, err.Error(), "WORKER_EVAL_DAILY_CAP")
	}
}

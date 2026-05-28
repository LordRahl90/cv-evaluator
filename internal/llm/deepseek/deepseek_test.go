package deepseek

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	code := 1
	defer func() {
		os.Exit(code)
	}()

	code = m.Run()
}

func TestService_CallLLM(t *testing.T) {
	key := "sk-3f601982cfe149538e5493c50f87a06b"
	svc := New(key)

	res, err := svc.callLLM(t.Context(), "why is the sky blue? list the response in json format")
	require.NoError(t, err)
	require.NotEmpty(t, res)

	fmt.Printf("\n\nResponse is: %s\n\n", res)
}

package config

import (
	"testing"
)

func TestExpandEnv(t *testing.T) {
	t.Setenv("MY_VAR", "hello")
	t.Setenv("EMPTY_VAR", "")

	tests := []struct {
		input string
		want  string
	}{
		// normal expansion
		{"$MY_VAR", "hello"},
		{"${MY_VAR}", "hello"},
		{"prefix_${MY_VAR}_suffix", "prefix_hello_suffix"},

		// undefined → empty string
		{"$UNDEFINED_XYZ", ""},
		{"${UNDEFINED_XYZ}", ""},

		// $$ escape → literal $
		{"$$", "$"},
		{"$${MY_VAR}", "${MY_VAR}"},
		{"$${LOG_LEVEL:-info}", "${LOG_LEVEL:-info}"},
		{"--flag=$${LOG_LEVEL:-info}", "--flag=${LOG_LEVEL:-info}"},

		// ${VAR:-default} — use default when unset or empty
		{"${UNDEFINED_XYZ:-fallback}", "fallback"},
		{"${EMPTY_VAR:-fallback}", "fallback"},
		{"${MY_VAR:-fallback}", "hello"},
		{"--log-level=${LOG_LEVEL:-info}", "--log-level=info"},
		{"--flag=${MY_VAR:-fallback}", "--flag=hello"},

		// $$ escape still passes ${...} through literally (container handles it)
		{"$${LOG_LEVEL:-info}", "${LOG_LEVEL:-info}"},

		// mixed: real expansion alongside escaped $
		{"$MY_VAR and $${RAW:-default}", "hello and ${RAW:-default}"},

		// no variables
		{"plain text", "plain text"},
		{"", ""},
	}

	for _, tc := range tests {
		got := expandEnv(tc.input)
		if got != tc.want {
			t.Errorf("expandEnv(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

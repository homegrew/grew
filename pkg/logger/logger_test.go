package logger

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestCLIHandler_Handle(t *testing.T) {
	tests := []struct {
		name     string
		level    slog.Level
		msg      string
		attrs    []slog.Attr
		expected string
	}{
		{
			name:     "error level",
			level:    slog.LevelError,
			msg:      "failed to open file",
			expected: "Error: failed to open file\n",
		},
		{
			name:     "warn level",
			level:    slog.LevelWarn,
			msg:      "deprecated configuration",
			expected: "Warning: deprecated configuration\n",
		},
		{
			name:     "info level",
			level:    slog.LevelInfo,
			msg:      "downloading package",
			expected: "    downloading package\n",
		},
		{
			name:     "debug level",
			level:    slog.LevelDebug,
			msg:      "cache miss",
			expected: "[debug] cache miss\n",
		},
		{
			name:  "with attributes",
			level: slog.LevelInfo,
			msg:   "process started",
			attrs: []slog.Attr{
				slog.String("pid", "1234"),
				slog.Bool("daemon", true),
			},
			expected: "    process started pid=1234 daemon=true\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			h := &CLIHandler{
				w:     &buf,
				level: slog.LevelDebug, // enable all for testing
			}

			r := slog.NewRecord(time.Now(), tt.level, tt.msg, 0)
			r.AddAttrs(tt.attrs...)

			err := h.Handle(context.Background(), r)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := buf.String(); got != tt.expected {
				t.Errorf("Handle() got %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestCLIHandler_Enabled(t *testing.T) {
	tests := []struct {
		name         string
		handlerLevel slog.Level
		testLevel    slog.Level
		want         bool
	}{
		{"warn handler allows warn", slog.LevelWarn, slog.LevelWarn, true},
		{"warn handler allows error", slog.LevelWarn, slog.LevelError, true},
		{"warn handler denies info", slog.LevelWarn, slog.LevelInfo, false},
		{"warn handler denies debug", slog.LevelWarn, slog.LevelDebug, false},

		{"info handler allows info", slog.LevelInfo, slog.LevelInfo, true},
		{"info handler allows warn", slog.LevelInfo, slog.LevelWarn, true},
		{"info handler denies debug", slog.LevelInfo, slog.LevelDebug, false},

		{"debug handler allows debug", slog.LevelDebug, slog.LevelDebug, true},
		{"debug handler allows info", slog.LevelDebug, slog.LevelInfo, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &CLIHandler{level: tt.handlerLevel}
			if got := h.Enabled(context.Background(), tt.testLevel); got != tt.want {
				t.Errorf("Enabled() got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCLIHandler_NoOps(t *testing.T) {
	h := &CLIHandler{}

	h2 := h.WithAttrs([]slog.Attr{slog.String("k", "v")})
	if h2 != h {
		t.Errorf("WithAttrs() should return the same handler, got %v", h2)
	}

	h3 := h.WithGroup("group")
	if h3 != h {
		t.Errorf("WithGroup() should return the same handler, got %v", h3)
	}
}

func TestTimeOp(t *testing.T) {
	// Temporarily override the default logger for testing
	var buf bytes.Buffer
	originalLogger := slog.Default()
	defer slog.SetDefault(originalLogger)

	// Enable debug logging
	h := &CLIHandler{w: &buf, level: slog.LevelDebug}
	slog.SetDefault(slog.New(h))

	// Run TimeOp
	done := TimeOp("test_op")

	// Ensure the start message is logged
	if !strings.Contains(buf.String(), "logger.go:") || !strings.Contains(buf.String(), "test_op started") {
		t.Errorf("Expected start message with source info, got: %q", buf.String())
	}

	buf.Reset()

	// Simulate time passing and call done
	time.Sleep(1 * time.Millisecond)
	done()

	// Ensure the completion message is logged
	if !strings.Contains(buf.String(), "logger.go:") || !strings.Contains(buf.String(), "test_op completed in") {
		t.Errorf("Expected completion message with source info, got: %q", buf.String())
	}
}

func TestTimeOp_Disabled(t *testing.T) {
	var buf bytes.Buffer
	originalLogger := slog.Default()
	defer slog.SetDefault(originalLogger)

	// Disable debug logging (set to Warn)
	h := &CLIHandler{w: &buf, level: slog.LevelWarn}
	slog.SetDefault(slog.New(h))

	done := TimeOp("test_op")
	done()

	if buf.Len() > 0 {
		t.Errorf("Expected no output when debug is disabled, got: %q", buf.String())
	}
}

func TestInit(t *testing.T) {
	originalLogger := slog.Default()
	defer slog.SetDefault(originalLogger)

	tests := []struct {
		name    string
		verbose bool
		debug   bool
		quiet   bool
		expected slog.Level
	}{
		{"default", false, false, false, slog.LevelWarn},
		{"verbose", true, false, false, slog.LevelInfo},
		{"debug", false, true, false, slog.LevelDebug},
		{"both", true, true, false, slog.LevelDebug}, // debug takes precedence
		{"quiet", false, false, true, slog.LevelError},
		{"quiet_prec", true, true, true, slog.LevelError}, // quiet takes precedence
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Init(tt.verbose, tt.debug, tt.quiet)

			// Verify the level set by checking Enabled on the default logger
			ctx := context.Background()

			// Should be enabled for the expected level and above
			if !slog.Default().Enabled(ctx, tt.expected) {
				t.Errorf("Expected level %v to be enabled", tt.expected)
			}

			// Should NOT be enabled for levels below the expected level
			if tt.expected > slog.LevelDebug && slog.Default().Enabled(ctx, tt.expected-1) {
				t.Errorf("Expected level %v to be disabled", tt.expected-1)
			}
		})
	}
}

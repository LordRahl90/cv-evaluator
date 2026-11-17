package extractor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

const (
	// ChildArg is the first argument that makes a binary run as an
	// extraction child (see RunChildIfRequested).
	ChildArg = "extract-pdf"

	// childMemoryBytes is the child's soft Go heap limit. The hard limit is
	// the container's memory limit: the child is the largest process in it,
	// so it is the one the kernel kills. An address-space limit (RLIMIT_AS)
	// is not used: the Go runtime reserves far more virtual memory than it
	// touches, and failed intermittently under one even for tiny files.
	childMemoryBytes = 512 << 20
	// childProcs caps the child's OS threads running Go code.
	childProcs = 2

	defaultTimeout     = 20 * time.Second
	defaultConcurrency = 2

	// Exit codes the child uses to report why it rejected a file.
	exitNotPDF       = 3
	exitTooLarge     = 4
	exitTooManyPages = 5
	exitUnreadable   = 6
)

// Isolated extracts text in a child process: the same binary, started with
// ChildArg. A file that hangs the parser is killed at the timeout, and one
// that exhausts memory only kills the child. At most a few run at once.
type Isolated struct {
	path    string
	timeout time.Duration
	slots   chan struct{}
}

// NewIsolated returns an Isolated that runs the current executable. The
// binary's main must call RunChildIfRequested first thing.
func NewIsolated() (*Isolated, error) {
	path, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return &Isolated{
		path:    path,
		timeout: defaultTimeout,
		slots:   make(chan struct{}, defaultConcurrency),
	}, nil
}

// Text returns the plain text of the PDF in data. See the package Text.
func (x *Isolated) Text(ctx context.Context, data []byte) (string, error) {
	if len(data) > MaxFileBytes {
		return "", ErrTooLarge
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return "", ErrNotPDF
	}

	select {
	case x.slots <- struct{}{}:
		defer func() { <-x.slots }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	ctx, cancel := context.WithTimeout(ctx, x.timeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, x.path, ChildArg)
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stdout = &limitedWriter{w: &stdout, n: MaxTextBytes}
	cmd.Stderr = &limitedWriter{w: &stderr, n: 4 << 10}
	// Only what the child needs: no secrets or config in its environment.
	cmd.Env = []string{}

	err := cmd.Run()
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("%w: extraction took longer than %s", ErrUnreadable, x.timeout)
		}
		return "", ctx.Err()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		switch exitErr.ExitCode() {
		case exitNotPDF:
			return "", ErrNotPDF
		case exitTooLarge:
			return "", ErrTooLarge
		case exitTooManyPages:
			return "", ErrTooManyPages
		case exitUnreadable:
			return "", fmt.Errorf("%w: %s", ErrUnreadable, strings.TrimSpace(stderr.String()))
		}
		// Killed (for example out of memory) or crashed. The child's stderr
		// says why; it goes to the server log, never to the client.
		slog.WarnContext(ctx, "pdf extraction child failed", "exit_code", exitErr.ExitCode(),
			"stderr", firstLines(stderr.String(), 5))
		return "", fmt.Errorf("%w: extraction failed: %v", ErrUnreadable, err)
	}
	if err != nil {
		return "", fmt.Errorf("start pdf extraction: %w", err)
	}
	return stdout.String(), nil
}

// RunChildIfRequested runs the extraction child and exits when the process
// was started with ChildArg; otherwise it returns at once. Call it first
// thing in main, before reading any configuration.
func RunChildIfRequested() {
	if len(os.Args) < 2 || os.Args[1] != ChildArg {
		return
	}
	os.Exit(runChild(os.Stdin, os.Stdout, os.Stderr))
}

func runChild(stdin io.Reader, stdout, stderr io.Writer) int {
	debug.SetMemoryLimit(childMemoryBytes)
	runtime.GOMAXPROCS(childProcs)

	data, err := io.ReadAll(io.LimitReader(stdin, MaxFileBytes+1))
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}

	text, err := Text(data)
	switch {
	case errors.Is(err, ErrNotPDF):
		return exitNotPDF
	case errors.Is(err, ErrTooLarge):
		return exitTooLarge
	case errors.Is(err, ErrTooManyPages):
		return exitTooManyPages
	case err != nil:
		_, _ = fmt.Fprintln(stderr, err)
		return exitUnreadable
	}

	if _, err := io.WriteString(stdout, text); err != nil {
		return 1
	}
	return 0
}

// firstLines returns at most n lines of s.
func firstLines(s string, n int) string {
	lines := strings.SplitN(strings.TrimSpace(s), "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// limitedWriter keeps the first n bytes written and drops the rest, so a
// misbehaving child cannot fill the parent's memory.
type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		if _, err := l.w.Write(p[:k]); err != nil {
			return 0, err
		}
		l.n -= k
	}
	return len(p), nil
}

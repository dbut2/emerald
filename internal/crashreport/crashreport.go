// A fault in the game thread is handled in C (port/hle/crash.c), which chains to
// Go's handler and kills the process, so no in-process Go code survives to
// report it. Hence the re-exec-and-supervise dance.
package crashreport

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
)

const (
	childEnv   = "EMERALD_SUPERVISED"
	disableEnv = "EMERALD_NO_CRASH_REPORT"
	tailBytes  = 64 << 10
)

// Guard never returns in the parent. Call it as the first statement in main,
// before any GUI or runtime setup.
func Guard() {
	if os.Getenv(childEnv) != "" || os.Getenv(disableEnv) != "" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}

	tail := newTailWriter(tailBytes)
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = append(os.Environ(), childEnv+"=1")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = teeWriter{os.Stderr, tail}

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "crashreport: cannot supervise: %v\n", err)
		os.Unsetenv(childEnv)
		return
	}

	// Ctrl-C reaches the child through the process group; keep the parent alive
	// long enough to reap it so a user interrupt isn't reported as a crash.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)
	go func() {
		for s := range sigs {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(s)
			}
		}
	}()

	err = cmd.Wait()
	if err == nil {
		os.Exit(0)
	}

	code, sig := waitStatus(cmd.ProcessState)
	if !interesting(sig) {
		os.Exit(code)
	}
	report(tail.String(), sig)
	os.Exit(code)
}

// A zero signal means the child exited nonzero on its own — usually a Go
// panic, which is worth filing.
func interesting(sig syscall.Signal) bool {
	switch sig {
	case syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT:
		return false
	}
	return true
}

func waitStatus(st *os.ProcessState) (code int, sig syscall.Signal) {
	if ws, ok := st.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal()), ws.Signal()
	}
	return st.ExitCode(), 0
}

func report(log string, sig syscall.Signal) {
	path := writeFullLog(log)
	url := IssueURL(Details{
		Log:      log,
		Signal:   sig,
		Args:     os.Args[1:],
		LogPath:  path,
		Revision: revision(),
		Go:       runtime.Version(),
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
	})

	fmt.Fprintf(os.Stderr, "\nemerald crashed. Full log: %s\n", path)
	if err := openBrowser(url); err != nil {
		fmt.Fprintf(os.Stderr, "File an issue: %s\n", url)
		return
	}
	fmt.Fprintf(os.Stderr, "Opening a prefilled issue at github.com/%s ...\n", repo)
}

func writeFullLog(log string) string {
	path := filepath.Join(os.TempDir(), fmt.Sprintf("emerald-crash-%d.log", os.Getpid()))
	if err := os.WriteFile(path, []byte(log), 0o644); err != nil {
		return ""
	}
	return path
}

func revision() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	var rev, dirty string
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				dirty = " (dirty)"
			}
		}
	}
	if rev == "" {
		return "unknown"
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	return rev + dirty
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

type teeWriter []interface{ Write([]byte) (int, error) }

func (t teeWriter) Write(p []byte) (int, error) {
	for _, w := range t {
		_, _ = w.Write(p)
	}
	return len(p), nil
}

type tailWriter struct {
	buf []byte
	max int
}

func newTailWriter(max int) *tailWriter { return &tailWriter{max: max} }

func (t *tailWriter) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailWriter) String() string { return strings.TrimRight(string(t.buf), "\n") }

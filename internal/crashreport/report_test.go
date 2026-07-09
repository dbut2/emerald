package crashreport

import (
	"net/url"
	"strings"
	"syscall"
	"testing"
)

const segvLog = `PE_TRACE keys=0000 cb2=CB2_Overworld
=== EMERALD C CRASH: SIGSEGV at fault addr 0x0 ===
0   emerald    0x0000000102a4c1b0 _ScriptContext_RunScript + 44
1   emerald    0x0000000102a4b0c4 _RunScriptCommand + 12
`

func TestTitle(t *testing.T) {
	tests := []struct {
		name string
		d    Details
		want string
	}{
		{"segv", Details{Log: segvLog}, "Crash: SIGSEGV in _ScriptContext_RunScript (null deref)"},
		{"fault addr", Details{Log: strings.Replace(segvLog, "0x0 ", "0xdeadbeef ", 1)},
			"Crash: SIGSEGV in _ScriptContext_RunScript (fault addr 0xdeadbeef)"},
		{"panic", Details{Log: "panic: runtime error: index out of range"},
			"Crash: panic: runtime error: index out of range"},
		{"bare signal", Details{Log: "no marker here", Signal: syscall.SIGABRT},
			"Crash: " + syscall.SIGABRT.String()},
		{"nothing", Details{Log: "no marker here"}, "Crash: emerald exited abnormally"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Title(tt.d); got != tt.want {
				t.Errorf("Title() = %q, want %q", got, tt.want)
			}
		})
	}
}

func realisticLog() string {
	return strings.Repeat("PE_TRACE keys=0000 cb2=CB2_Overworld\n", 2000) +
		segvLog +
		strings.Repeat("goroutine 42 gp=0x4ca3f gopark runtime/proc.go:462 +0xbc\n", 2000)
}

func TestIssueURLFitsAndKeepsTheCrash(t *testing.T) {
	u := IssueURL(Details{
		Log:      realisticLog(),
		Revision: "abc123def456",
		Platform: "darwin/arm64",
		Go:       "go1.26",
	})
	if len(u) > maxURL {
		t.Fatalf("URL is %d bytes, want <= %d", len(u), maxURL)
	}

	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatalf("URL does not parse: %v", err)
	}
	body := parsed.Query().Get("body")
	for _, want := range []string{
		"EMERALD C CRASH",
		"_ScriptContext_RunScript",
		"_RunScriptCommand",
		elision,
		"abc123def456",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("trimmed body dropped %q", want)
		}
	}
	if strings.Contains(body, "goroutine 42") {
		t.Error("trimmed body kept Go's goroutine dump instead of the C backtrace")
	}
	if got := parsed.Query().Get("title"); !strings.HasPrefix(got, "Crash: SIGSEGV") {
		t.Errorf("title = %q", got)
	}
	if got := parsed.Query().Get("labels"); got != triageLabel {
		t.Errorf("labels = %q, want %q — the autofix workflow triggers on it", got, triageLabel)
	}
}

func TestExcerptKeepsPrecedingTraceContext(t *testing.T) {
	got := excerpt(realisticLog(), 1500)
	if !strings.Contains(got, "PE_TRACE") {
		t.Error("excerpt dropped the PE_TRACE context preceding the crash")
	}
	if !strings.HasPrefix(got, elision) {
		t.Errorf("excerpt should mark the elided head, got %.40q", got)
	}
}

func TestExcerptWithoutAnchorFallsBackToTail(t *testing.T) {
	log := strings.Repeat("noise\n", 500) + "last meaningful line\n"
	got := excerpt(log, 100)
	if !strings.Contains(got, "last meaningful line") {
		t.Error("anchorless excerpt must keep the tail")
	}
}

func TestIssueURLShortLogIsNotTruncated(t *testing.T) {
	u := IssueURL(Details{Log: segvLog})
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatal(err)
	}
	if body := parsed.Query().Get("body"); strings.Contains(body, "…truncated…") {
		t.Error("short log should not be truncated")
	}
}

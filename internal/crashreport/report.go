package crashreport

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"syscall"
)

const (
	repo        = "dbut2/emerald"
	triageLabel = "crash"
)

const maxURL = 6000

type Details struct {
	Log      string
	Signal   syscall.Signal
	Args     []string
	LogPath  string
	Revision string
	Go       string
	Platform string
}

var (
	crashRe  = regexp.MustCompile(`=== EMERALD C CRASH: (\w+)(?: at fault addr (0x[0-9a-f]+))?`)
	frameRe  = regexp.MustCompile(`(?m)^\s*\d+\s+\S+\s+0x[0-9a-f]+\s+(\S+)`)
	panicRe  = regexp.MustCompile(`(?m)^panic: (.+)$`)
	fatalRe  = regexp.MustCompile(`(?m)^fatal error: .+$`)
	goDumpRe = regexp.MustCompile(`(?m)^(goroutine \d+|SIG[A-Z]+: |\[signal SIG)`)
)

func IssueURL(d Details) string {
	title, full := Title(d), d.Log
	for _, budget := range []int{6000, 3000, 1500, 750, 0} {
		d.Log = excerpt(full, budget)
		if u := issueURL(title, body(d)); len(u) <= maxURL {
			return u
		}
	}
	d.Log = ""
	return issueURL(title, body(d))
}

func issueURL(title, body string) string {
	q := url.Values{"title": {title}, "body": {body}, "labels": {triageLabel}}
	return "https://github.com/" + repo + "/issues/new?" + q.Encode()
}

func Title(d Details) string {
	if m := panicRe.FindStringSubmatch(d.Log); m != nil {
		return "Crash: panic: " + truncLine(m[1], 80)
	}
	m := crashRe.FindStringSubmatch(d.Log)
	if m == nil {
		if d.Signal != 0 {
			return "Crash: " + d.Signal.String()
		}
		return "Crash: emerald exited abnormally"
	}
	title := "Crash: " + m[1]
	if fn := topFrame(d.Log[strings.Index(d.Log, m[0]):]); fn != "" {
		title += " in " + fn
	}
	if m[2] != "" && m[2] != "0x0" {
		title += " (fault addr " + m[2] + ")"
	} else if m[2] == "0x0" {
		title += " (null deref)"
	}
	return title
}

func topFrame(logFromCrash string) string {
	m := frameRe.FindStringSubmatch(logFromCrash)
	if m == nil {
		return ""
	}
	return m[1]
}

func body(d Details) string {
	var b strings.Builder
	b.WriteString("Filed automatically by emerald's crash reporter.\n\n")

	b.WriteString("| | |\n|-|-|\n")
	fmt.Fprintf(&b, "| commit | `%s` |\n", d.Revision)
	fmt.Fprintf(&b, "| platform | `%s` |\n", d.Platform)
	fmt.Fprintf(&b, "| go | `%s` |\n", d.Go)
	fmt.Fprintf(&b, "| args | `%s` |\n", argsOrNone(d.Args))
	if d.Signal != 0 {
		fmt.Fprintf(&b, "| signal | `%s` |\n", d.Signal)
	}

	b.WriteString("\n### What were you doing?\n\n_Replace this with what was happening in-game._\n")

	if d.Log != "" {
		b.WriteString("\n### Crash log\n\n```\n")
		b.WriteString(d.Log)
		b.WriteString("\n```\n")
	}
	if d.LogPath != "" {
		fmt.Fprintf(&b, "\nFull log on the reporter's machine: `%s`\n", d.LogPath)
	}
	return b.String()
}

func argsOrNone(args []string) string {
	if len(args) == 0 {
		return "(none)"
	}
	return strings.Join(args, " ")
}

const elision = "…truncated…"

// Anchored on the marker, not the tail: the C backtrace prints first, then Go's
// chained handler buries it under every goroutine.
func excerpt(log string, budget int) string {
	if budget <= 0 {
		return ""
	}
	if len(log) <= budget {
		return log
	}
	at, isC := anchor(log)
	if at < 0 {
		return elision + "\n" + tailLines(log, budget)
	}

	pre := contextBefore(log[:at], 3)
	post := log[at:]
	if isC {
		if loc := goDumpRe.FindStringIndex(post); loc != nil {
			post = post[:loc[0]] + elision + "\n"
		}
	}
	if room := budget - len(pre); len(post) > room {
		post = strings.TrimRight(post[:max(room, 0)], "\n") + "\n" + elision
	}
	if at > len(pre) {
		pre = elision + "\n" + pre
	}
	return pre + post
}

func anchor(log string) (at int, isC bool) {
	best := -1
	for i, re := range []*regexp.Regexp{crashRe, panicRe, fatalRe} {
		if loc := re.FindStringIndex(log); loc != nil && (best < 0 || loc[0] < best) {
			best, isC = loc[0], i == 0
		}
	}
	if best < 0 {
		return -1, false
	}
	return lineStart(log, best), isC
}

func lineStart(s string, i int) int {
	if nl := strings.LastIndexByte(s[:i], '\n'); nl >= 0 {
		return nl + 1
	}
	return 0
}

func contextBefore(head string, lines int) string {
	head = strings.TrimRight(head, "\n")
	if head == "" {
		return ""
	}
	i := len(head)
	for range lines {
		nl := strings.LastIndexByte(head[:i], '\n')
		if nl < 0 {
			i = 0
			break
		}
		i = nl
	}
	return strings.TrimLeft(head[i:], "\n") + "\n"
}

func tailLines(log string, budget int) string {
	tail := log[len(log)-budget:]
	if nl := strings.IndexByte(tail, '\n'); nl >= 0 {
		tail = tail[nl+1:]
	}
	return tail
}

func truncLine(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

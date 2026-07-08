// Symbolized C backtrace for faults inside the pokeemerald core.
//
// The game runs on its own pthread (see bridge.c), which the Go runtime does
// not manage. When it faults, Go's handler catches the signal on a foreign
// thread and dies with the unhelpful "signal arrived during cgo execution".
// pe_install_crash_handler runs after the Go runtime is up (called from the
// game thread), so its sigaction overrides Go's; the handler dumps a C
// backtrace, then chains to Go's saved handler so Go still reports and exits.
#include <execinfo.h>
#include <mach-o/dyld.h>
#include <signal.h>
#include <stdint.h>
#include <string.h>
#include <sys/ucontext.h>
#include <unistd.h>

// Captured at install time so the handler itself stays async-signal-safe.
static char g_exe_path[1024];
static uintptr_t g_load_addr;
static struct sigaction g_prev[NSIG];

static const int kSigs[] = {SIGSEGV, SIGBUS, SIGILL, SIGFPE, SIGABRT, SIGTRAP};

static void wstr(const char *s) { write(STDERR_FILENO, s, strlen(s)); }

static void whex(uintptr_t v)
{
    char buf[2 + 16];
    char *p = buf + sizeof(buf);
    static const char hex[] = "0123456789abcdef";
    do {
        *--p = hex[v & 0xf];
        v >>= 4;
    } while (v);
    *--p = 'x';
    *--p = '0';
    write(STDERR_FILENO, p, buf + sizeof(buf) - p);
}

static const char *signame(int sig)
{
    switch (sig) {
    case SIGSEGV: return "SIGSEGV";
    case SIGBUS:  return "SIGBUS";
    case SIGILL:  return "SIGILL";
    case SIGFPE:  return "SIGFPE";
    case SIGABRT: return "SIGABRT";
    case SIGTRAP: return "SIGTRAP";
    default:      return "signal";
    }
}

static uintptr_t fault_pc(void *ctx)
{
    ucontext_t *uc = ctx;
    if (!uc || !uc->uc_mcontext)
        return 0;
#if defined(__aarch64__)
    return uc->uc_mcontext->__ss.__pc;
#elif defined(__x86_64__)
    return uc->uc_mcontext->__ss.__rip;
#else
    return 0;
#endif
}

static void crash_handler(int sig, siginfo_t *info, void *ctx)
{
    wstr("\n=== EMERALD C CRASH: ");
    wstr(signame(sig));
    if (info) {
        wstr(" at fault addr ");
        whex((uintptr_t)info->si_addr);
    }
    wstr(" ===\n");

    // backtrace() walks from the handler and the signal trampoline drops the
    // faulting leaf frame, so surface the interrupted PC from ctx explicitly.
    uintptr_t pc = fault_pc(ctx);
    void *frames[65];
    int n = backtrace(frames + 1, 64);
    int off = 0;
    if (pc) {
        frames[0] = (void *)pc;
        off = 1;
    }
    int start = off ? 0 : 1;
    int count = n + off;
    backtrace_symbols_fd(frames + start, count, STDERR_FILENO);

    wstr("\nsymbolize static frames with:\n  atos -o ");
    wstr(g_exe_path);
    wstr(" -l ");
    whex(g_load_addr);
    for (int i = start; i < start + count; i++) {
        wstr(" ");
        whex((uintptr_t)frames[i]);
    }
    wstr("\n\n");

    // Chain to whatever was installed before us (Go's handler) so the runtime
    // still dumps goroutines and terminates with the right disposition.
    struct sigaction *prev = &g_prev[sig];
    if (prev->sa_flags & SA_SIGINFO) {
        if (prev->sa_sigaction) {
            prev->sa_sigaction(sig, info, ctx);
            return;
        }
    } else if (prev->sa_handler != SIG_DFL && prev->sa_handler != SIG_IGN) {
        prev->sa_handler(sig);
        return;
    }
    signal(sig, SIG_DFL);
    raise(sig);
}

void pe_install_crash_handler(void)
{
    uint32_t sz = sizeof(g_exe_path);
    if (_NSGetExecutablePath(g_exe_path, &sz) != 0)
        g_exe_path[0] = '\0';
    g_load_addr = (uintptr_t)_dyld_get_image_header(0);

    // sigaltstack is per-thread on macOS, so this must run on the game thread.
    static _Thread_local char altstack[SIGSTKSZ];
    stack_t ss = {.ss_sp = altstack, .ss_size = sizeof(altstack), .ss_flags = 0};
    sigaltstack(&ss, NULL);

    struct sigaction sa;
    memset(&sa, 0, sizeof(sa));
    sa.sa_sigaction = crash_handler;
    sa.sa_flags = SA_SIGINFO | SA_ONSTACK;
    sigemptyset(&sa.sa_mask);
    for (unsigned i = 0; i < sizeof(kSigs) / sizeof(kSigs[0]); i++)
        sigaction(kSigs[i], &sa, &g_prev[kSigs[i]]);
}

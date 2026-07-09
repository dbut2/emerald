#include <stdint.h>
#include <stdlib.h>
#include <stdio.h>
#include <dlfcn.h>
#include <string.h>
#include <pthread.h>
#include "global.h"
#include "global.fieldmap.h"
#include "main.h"
#include "task.h"

static const char *symof(void *p)
{
    // dladdr is a symbol-table walk (slow); the same handful of callback/task
    // addresses recur every frame, so cache resolved names to keep PE_TRACE cheap.
    static struct { void *p; const char *n; } cache[128];
    static int ncache;
    if (!p)
        return "?";
    for (int i = 0; i < ncache; i++)
        if (cache[i].p == p)
            return cache[i].n;
    Dl_info info;
    const char *n = (dladdr(p, &info) && info.dli_sname) ? info.dli_sname : "?";
    if (ncache < (int)(sizeof cache / sizeof cache[0]))
        cache[ncache].p = p, cache[ncache].n = n, ncache++;
    return n;
}

extern void gba_mem_init(void);
extern void CB2_InitCopyrightScreenAfterBootup(void);
extern void pe_install_crash_handler(void);

#define HOST(a) ((a) + 0x1FFFFE000000UL)

// AgbMain owns an infinite loop, so it runs on its own thread and the patched
// WaitForVBlank (port_frame_end) hands off one frame per pe_run_frame via this
// go/done rendezvous. Single game thread => the game state stays race-free.
static pthread_mutex_t g_m = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t g_c = PTHREAD_COND_INITIALIZER;
static int g_go, g_done, g_frames, g_remaining;

static void *game_thread(void *a)
{
    (void)a;
    pe_install_crash_handler();
    AgbMain();
    return NULL;
}


const char *pe_symof_ret(void *p)
{
    return symof(p);
}

void pe_init(void)
{
    extern void pe_flash_init(void);
    gba_mem_init();
    pe_flash_init();
    *(volatile uint16_t *)HOST(0x4000130UL) = 0x03FF;
    pthread_t t;
    pthread_create(&t, NULL, game_thread, NULL);
    pthread_mutex_lock(&g_m);
    while (!g_done)
        pthread_cond_wait(&g_c, &g_m);
    g_done = 0;
    pthread_mutex_unlock(&g_m);
}

// n frames per go/done handoff: only the nth crosses back (see port_frame_end),
// so the bridge rendezvous amortises across the batch instead of gating each frame.
void pe_run_frames(uint16_t keys, int n)
{
    *(volatile uint16_t *)HOST(0x4000130UL) = keys;
    pthread_mutex_lock(&g_m);
    g_remaining = n;
    g_go = 1;
    pthread_cond_signal(&g_c);
    while (!g_done)
        pthread_cond_wait(&g_c, &g_m);
    g_done = 0;
    pthread_mutex_unlock(&g_m);
}

void pe_run_frame(uint16_t keys)
{
    pe_run_frames(keys, 1);
}

unsigned char *pe_base(void)
{
    return (unsigned char *)HOST(0x2000000UL);
}

void port_frame_end(void)
{
    // Flash reads absent on host, which nulls callback2; re-arm the boot chain.
    if (g_frames == 0 && gMain.callback2 == NULL)
        SetMainCallback2(CB2_InitCopyrightScreenAfterBootup);
    static int trace = -1, postrace = -1;
    if (trace < 0)
        trace = getenv("PE_TRACE") != NULL, postrace = getenv("PE_POS") != NULL;
    if (trace) {
        // Emit only when the frame signature changes: the overworld holds the same
        // cb2/tasks for thousands of frames, so per-frame I/O (150k lines/s) is what
        // tanks throughput. Deduping keeps the crash context at ~1/1000 the cost.
        static char line[600], last[600];
        int o = snprintf(line, sizeof line, "keys=%04x cb2=%s cb1=%s |",
                (unsigned)*(volatile uint16_t *)HOST(0x4000130UL),
                symof((void *)gMain.callback2), symof((void *)gMain.callback1));
        for (int i = 0; i < NUM_TASKS && o < (int)sizeof line - 40; i++)
            if (gTasks[i].isActive)
                o += snprintf(line + o, sizeof line - o, " %s", symof((void *)gTasks[i].func));
        if (strcmp(line, last) != 0) {
            fprintf(stderr, "f%d %s\n", g_frames, line);
            __builtin_memcpy(last, line, o + 1);
        }
    }
    if (postrace) {
        struct ObjectEvent *p = &gObjectEvents[gPlayerAvatar.objectEventId];
        fprintf(stderr, "f%d pos=(%d,%d) face=%d\n", g_frames,
                p->currentCoords.x - 7, p->currentCoords.y - 7, p->facingDirection);
    }
    { extern void pe_flash_flush(void); pe_flash_flush(); }
    g_frames++;

    if (--g_remaining > 0)
        return;

    pthread_mutex_lock(&g_m);
    g_done = 1;
    pthread_cond_signal(&g_c);
    while (!g_go)
        pthread_cond_wait(&g_c, &g_m);
    g_go = 0;
    pthread_mutex_unlock(&g_m);
}

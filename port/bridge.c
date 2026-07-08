#include <stdint.h>
#include <pthread.h>
#include "global.h"
#include "main.h"

extern void gba_mem_init(void);
extern void CB2_InitCopyrightScreenAfterBootup(void);
extern void pe_install_crash_handler(void);

#define HOST(a) ((a) + 0x3FE000000UL)

// AgbMain owns an infinite loop, so it runs on its own thread and the patched
// WaitForVBlank (port_frame_end) hands off one frame per pe_run_frame via this
// go/done rendezvous. Single game thread => the game state stays race-free.
static pthread_mutex_t g_m = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t g_c = PTHREAD_COND_INITIALIZER;
static int g_go, g_done, g_frames;

static void *game_thread(void *a)
{
    (void)a;
    pe_install_crash_handler();
    AgbMain();
    return NULL;
}

void pe_init(void)
{
    gba_mem_init();
    *(volatile uint16_t *)HOST(0x4000130UL) = 0x03FF;
    pthread_t t;
    pthread_create(&t, NULL, game_thread, NULL);
    pthread_mutex_lock(&g_m);
    while (!g_done)
        pthread_cond_wait(&g_c, &g_m);
    g_done = 0;
    pthread_mutex_unlock(&g_m);
}

void pe_run_frame(uint16_t keys)
{
    *(volatile uint16_t *)HOST(0x4000130UL) = keys;
    pthread_mutex_lock(&g_m);
    g_go = 1;
    pthread_cond_signal(&g_c);
    while (!g_done)
        pthread_cond_wait(&g_c, &g_m);
    g_done = 0;
    pthread_mutex_unlock(&g_m);
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
    g_frames++;
    pthread_mutex_lock(&g_m);
    g_done = 1;
    pthread_cond_signal(&g_c);
    while (!g_go)
        pthread_cond_wait(&g_c, &g_m);
    g_go = 0;
    pthread_mutex_unlock(&g_m);
}

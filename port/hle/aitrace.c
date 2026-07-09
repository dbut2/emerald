#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <stdint.h>

typedef unsigned char u8;

// A wild gAIScriptPtr is a benign no-op read on the MMU-less GBA but faults on
// host; probe readability before the interpreter dereferences it and, on a bad
// pointer, dump recent opcodes so the corrupting transition is visible.
#define RING 24
static struct { const u8 *ptr; unsigned op; } sRing[RING];
static int sRingN;

// mincore reports PROT_NONE/guard pages as "mapped" and /dev/null discards without
// copying, so neither gates a read. Writing into a pipe forces the kernel to copy
// (validate) the byte: EFAULT (write != 1) means unreadable, without faulting us.
static int readable(const void *p)
{
    static int pfd[2] = {-1, -1};
    if (pfd[0] < 0 && pipe(pfd) != 0) return 1;
    if (write(pfd[1], p, 1) != 1) return 0;
    char drain;
    (void)!read(pfd[0], &drain, 1);
    return 1;
}

// Returns 1 if gAIScriptPtr is safe to dereference, 0 if wild (caller must bail).
int pe_ai_guard(const u8 *ptr, unsigned logic, unsigned idx)
{
    static int trace = -1;
    if (trace < 0) trace = getenv("PE_AI") != NULL;

    if (!readable(ptr)) {
        fprintf(stderr, "AI WILD gAIScriptPtr=%p logic=%u idx=%u -- recent opcodes:\n",
                (const void *)ptr, logic, idx);
        int lo = sRingN > RING ? sRingN - RING : 0;
        for (int i = lo; i < sRingN; i++)
            fprintf(stderr, "  op=0x%02x @ %p\n", sRing[i % RING].op, (const void *)sRing[i % RING].ptr);
        fflush(stderr);
        return 0;
    }

    unsigned op = *ptr;
    sRing[sRingN % RING].ptr = ptr;
    sRing[sRingN % RING].op = op;
    sRingN++;
    if (trace)
        fprintf(stderr, "AIop=%02x ptr=%p logic=%u idx=%u\n", op, (const void *)ptr, logic, idx);
    return 1;
}

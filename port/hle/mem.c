#include <sys/mman.h>
#include <stdio.h>
#include <stdlib.h>

// GBA physical map [0x02000000,0x08000000) mapped at a fixed high host base so
// the rebased address macros (io_reg.h/defines.h, +0x3FE000000) stay
// compile-time constants and resolve to real memory. macOS reserves the low
// addresses, so a high MAP_FIXED base is required.
#define GBA_LO   0x02000000UL
#define GBA_SPAN 0x0E000000UL
#define HOST_BASE 0x400000000UL

void gba_mem_init(void)
{
    void *p = mmap((void *)HOST_BASE, GBA_SPAN, PROT_READ | PROT_WRITE,
                   MAP_PRIVATE | MAP_ANON | MAP_FIXED, -1, 0);
    if (p != (void *)HOST_BASE)
    {
        fprintf(stderr, "gba_mem_init: mmap failed (%p)\n", p);
        exit(1);
    }
}

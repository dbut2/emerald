#ifndef GUARD_PORT_GBA_DEFINES_H
#define GUARD_PORT_GBA_DEFINES_H

// Include upstream by path (not name) so -iquote resolves this shadow, not it.
#include "../../../pokeemerald/include/gba/defines.h"

// mach-o/host ELF have no ewram/iwram sections; neuter placement to host memory.

#undef IWRAM_DATA
#undef EWRAM_DATA
#undef COMMON_DATA
#define IWRAM_DATA
#define EWRAM_DATA
#define COMMON_DATA

#ifndef GBA_HOST_DELTA
#define GBA_HOST_DELTA 0x3FE000000UL
#endif

// Rebase the base region addresses; derived macros (BG_VRAM, OBJ_PLTT, ...)
// follow at use time and stay compile-time constants.
#undef PLTT
#undef VRAM
#undef OAM
#undef EWRAM_START
#undef EWRAM_END
#undef IWRAM_START
#undef IWRAM_END
#undef SOUND_INFO_PTR
#undef INTR_CHECK
#undef INTR_VECTOR
#define PLTT        (0x5000000 + GBA_HOST_DELTA)
#define VRAM        (0x6000000 + GBA_HOST_DELTA)
#define OAM         (0x7000000 + GBA_HOST_DELTA)
#define EWRAM_START (0x2000000 + GBA_HOST_DELTA)
#define EWRAM_END   (EWRAM_START + 0x40000)
#define IWRAM_START (0x3000000 + GBA_HOST_DELTA)
#define IWRAM_END   (IWRAM_START + 0x8000)
#define SOUND_INFO_PTR (*(struct SoundInfo **)(0x3007FF0 + GBA_HOST_DELTA))
#define INTR_CHECK     (*(u16 *)(0x3007FF8 + GBA_HOST_DELTA))
#define INTR_VECTOR    (*(void **)(0x3007FFC + GBA_HOST_DELTA))

#endif // GUARD_PORT_GBA_DEFINES_H

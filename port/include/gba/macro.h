#ifndef GUARD_PORT_GBA_MACRO_H
#define GUARD_PORT_GBA_MACRO_H

#include "../../../pokeemerald/include/gba/macro.h"

#if PORT_HOST
extern void port_dma(const void *src, void *dest, u32 control);

#undef DmaSetUnchecked
#define DmaSetUnchecked(dmaNum, src, dest, control) \
    port_dma((const void *)(src), (void *)(dest), (u32)(control))
#endif

#endif // GUARD_PORT_GBA_MACRO_H

#include "global.h"
#include "gba/gba.h"

// No hardware DMA controller exists on host, so the shadowed DmaSetUnchecked
// (port/include/gba/macro.h) routes control-word writes here. Only immediate
// (START_NOW) transfers run; timed channels (scanline effects, sound FIFO) are
// left inert, matching the port's once-per-frame model.
void port_dma(const void *src, void *dest, u32 control)
{
    u32 count = control & 0xFFFF;
    u32 flags = control >> 16;

    if (!(flags & DMA_ENABLE) || (flags & DMA_START_MASK) || count == 0)
        return;

    if (flags & DMA_32BIT)
    {
        u32 *d = dest; const u32 *s = src;
        if (flags & DMA_SRC_FIXED) { u32 v = *s; for (u32 i = 0; i < count; i++) d[i] = v; }
        else                       for (u32 i = 0; i < count; i++) d[i] = s[i];
    }
    else
    {
        u16 *d = dest; const u16 *s = src;
        if (flags & DMA_SRC_FIXED) { u16 v = *s; for (u32 i = 0; i < count; i++) d[i] = v; }
        else                       for (u32 i = 0; i < count; i++) d[i] = s[i];
    }
}

#include "global.h"
#include "gba/gba.h"
#include <math.h>
#include <string.h>

// The MODERN CpuSet/CpuFastSet macros rewrite the call token; undo them so the
// definitions below declare the real symbols the game links against.
#undef CpuSet
#undef CpuFastSet

void CpuSet(const void *src, void *dest, u32 control)
{
    u32 count = control & 0x1FFFFF;
    u32 fixed = control & CPU_SET_SRC_FIXED;
    if (control & CPU_SET_32BIT)
    {
        const u32 *s = src; u32 *d = dest;
        if (fixed) { u32 v = *s; for (u32 i = 0; i < count; i++) d[i] = v; }
        else       for (u32 i = 0; i < count; i++) d[i] = s[i];
    }
    else
    {
        const u16 *s = src; u16 *d = dest;
        if (fixed) { u16 v = *s; for (u32 i = 0; i < count; i++) d[i] = v; }
        else       for (u32 i = 0; i < count; i++) d[i] = s[i];
    }
}

void CpuFastSet(const void *src, void *dest, u32 control)
{
    u32 count = control & 0x1FFFFF;
    const u32 *s = src; u32 *d = dest;
    if (control & CPU_FAST_SET_SRC_FIXED) { u32 v = *s; for (u32 i = 0; i < count; i++) d[i] = v; }
    else                                  for (u32 i = 0; i < count; i++) d[i] = s[i];
}

static void LZ77UnComp(const u32 *src, void *dest)
{
    const u8 *s = (const u8 *)src;
    u8 *d = dest;
    u32 size = (s[1] | (s[2] << 8) | (s[3] << 16));
    s += 4;
    u32 w = 0;
    while (w < size)
    {
        u8 flags = *s++;
        for (int i = 0; i < 8 && w < size; i++)
        {
            if (flags & 0x80)
            {
                u8 b1 = *s++, b2 = *s++;
                u32 disp = (((b1 & 0x0F) << 8) | b2) + 1;
                u32 len = (b1 >> 4) + 3;
                for (u32 j = 0; j < len && w < size; j++, w++)
                    d[w] = d[w - disp];
            }
            else
            {
                d[w++] = *s++;
            }
            flags <<= 1;
        }
    }
}
void LZ77UnCompWram(const u32 *src, void *dest) { LZ77UnComp(src, dest); }
void LZ77UnCompVram(const u32 *src, void *dest) { LZ77UnComp(src, dest); }

static void RLUnComp(const u32 *src, void *dest)
{
    const u8 *s = (const u8 *)src;
    u8 *d = dest;
    u32 size = (s[1] | (s[2] << 8) | (s[3] << 16));
    s += 4;
    u32 w = 0;
    while (w < size)
    {
        u8 flag = *s++;
        if (flag & 0x80)
        {
            u32 len = (flag & 0x7F) + 3;
            u8 v = *s++;
            for (u32 j = 0; j < len && w < size; j++) d[w++] = v;
        }
        else
        {
            u32 len = (flag & 0x7F) + 1;
            for (u32 j = 0; j < len && w < size; j++) d[w++] = *s++;
        }
    }
}
void RLUnCompWram(const u32 *src, void *dest) { RLUnComp(src, dest); }
void RLUnCompVram(const u32 *src, void *dest) { RLUnComp(src, dest); }

u16 Sqrt(u32 num)
{
    u32 r = (u32)sqrt((double)num);
    while ((u64)(r + 1) * (r + 1) <= num) r++;
    while (r != 0 && (u64)r * r > num) r--;
    return (u16)r;
}

s32 Div(s32 num, s32 denom) { return num / denom; }

u16 ArcTan2(s16 x, s16 y)
{
    double a = atan2((double)y, (double)x);
    if (a < 0) a += 2.0 * M_PI;
    return (u16)(a / (2.0 * M_PI) * 65536.0);
}

// libm trig keyed off the top 8 bits (BIOS uses a 256-entry table); tighten to
// the exact BIOS sine table against the oracle when affine scenes are reached.
static void affine_trig(u16 alpha, double *c, double *s)
{
    double t = ((double)(alpha >> 8) / 256.0) * 2.0 * M_PI;
    *c = cos(t); *s = sin(t);
}

void BgAffineSet(struct BgAffineSrcData *src, struct BgAffineDstData *dest, s32 count)
{
    for (s32 i = 0; i < count; i++, src++, dest++)
    {
        double c, s;
        affine_trig(src->alpha, &c, &s);
        s16 pa = (s16)(src->sx * c);
        s16 pb = (s16)(-src->sx * s);
        s16 pc = (s16)(src->sy * s);
        s16 pd = (s16)(src->sy * c);
        dest->pa = pa; dest->pb = pb; dest->pc = pc; dest->pd = pd;
        dest->dx = src->texX - (pa * src->scrX + pb * src->scrY);
        dest->dy = src->texY - (pc * src->scrX + pd * src->scrY);
    }
}

void ObjAffineSet(struct ObjAffineSrcData *src, void *dest, s32 count, s32 offset)
{
    u8 *d = dest;
    for (s32 i = 0; i < count; i++, src++)
    {
        double c, s;
        affine_trig(src->rotation, &c, &s);
        s16 v[4] = {
            (s16)(src->xScale * c), (s16)(-src->xScale * s),
            (s16)(src->yScale * s), (s16)(src->yScale * c),
        };
        for (int k = 0; k < 4; k++, d += offset) *(s16 *)d = v[k];
    }
}

void RegisterRamReset(u32 resetFlags) { (void)resetFlags; }
void SoftReset(u32 resetFlags)        { (void)resetFlags; }
void VBlankIntrWait(void)             { }
int MultiBoot(struct MultiBootParam *mp) { (void)mp; return 1; }

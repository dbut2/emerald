#ifndef GUARD_PORT_GLOBAL_H
#define GUARD_PORT_GLOBAL_H

#include "../../pokeemerald/include/global.h"

// STATIC_ASSERT guards ILP32 struct sizes (SaveBlock fits flash, overlay fits
// Task.data). Those sizes legitimately differ under LP64; the real save-size
// constraint moves into field-wise save serialization. Neuter on host only.
#undef STATIC_ASSERT
#define STATIC_ASSERT(expr, id)

// global.h stubs INCBIN to {0} under __APPLE__ to "fool" IDE preproc, but the
// host build compiles with Apple clang, so that stub silently zeroed every
// INCBIN asset (e.g. gTitleScreenBgPalettes). Undo it: leave INCBIN unexpanded
// so tools/preproc emits the real file bytes, as it already does for INCGFX.
#undef INCBIN
#undef INCBIN_U8
#undef INCBIN_U16
#undef INCBIN_U32
#undef INCBIN_S8
#undef INCBIN_S16
#undef INCBIN_S32

// Same __APPLE__ trap for the charmap macros: _()/__() get stubbed to {x}, so
// clang -E expands them before tools/preproc can charmap-encode the literal, and
// every string ends up as raw ASCII bytes the in-game font can't map (dialog
// text renders as garbage). Undo it so preproc sees _("...") and emits real
// charmap bytes.
#undef _
#undef __

// T*_READ_PTR read a pointer from a byte stream (map scripts, converted data
// tables). The data converter stores those pointers at sizeof(void*), so a
// 4-byte T*_READ_32 truncates on LP64; read pointer-width instead. Callers that
// also stride manually over such tables must step by sizeof(void*), not 4.
static inline void *port_read_ptr(const void *p)
{
    void *r;
    __builtin_memcpy(&r, p, sizeof(r));
    return r;
}
#undef T1_READ_PTR
#define T1_READ_PTR(ptr) ((u8 *)port_read_ptr(ptr))
#undef T2_READ_PTR
#define T2_READ_PTR(ptr) ((void *)port_read_ptr(ptr))

#endif // GUARD_PORT_GLOBAL_H

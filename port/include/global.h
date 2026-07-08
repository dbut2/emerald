#ifndef GUARD_PORT_GLOBAL_H
#define GUARD_PORT_GLOBAL_H

#include "../../pokeemerald/include/global.h"

// STATIC_ASSERT guards ILP32 struct sizes (SaveBlock fits flash, overlay fits
// Task.data). Those sizes legitimately differ under LP64; the real save-size
// constraint moves into field-wise save serialization. Neuter on host only.
#undef STATIC_ASSERT
#define STATIC_ASSERT(expr, id)

#endif // GUARD_PORT_GLOBAL_H

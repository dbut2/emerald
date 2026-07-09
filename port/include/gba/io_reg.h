#ifndef GUARD_PORT_GBA_IO_REG_H
#define GUARD_PORT_GBA_IO_REG_H

#include "../../../pokeemerald/include/gba/io_reg.h"

#ifndef GBA_HOST_DELTA
#define GBA_HOST_DELTA 0x1FFFFE000000UL
#endif

// REG_ADDR_*/REG_* derive from REG_BASE at use time, so rebasing it alone
// redirects every register access into the host-mapped I/O page.
#undef REG_BASE
#define REG_BASE (0x4000000 + GBA_HOST_DELTA)

#endif // GUARD_PORT_GBA_IO_REG_H

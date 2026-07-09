#include <string.h>
#include <stdio.h>
#include <stdlib.h>
#include "global.h"
#include "gba/gba.h"
#include "gba/flash_internal.h"
#include "agb_flash.h"

// The GBA Flash-1M driver runs a self-modifying routine copied into IWRAM and
// talks to the cartridge bus, so agb_flash*.o is dropped from the link
// (port/build.sh) and replaced by a flat 128 KB buffer. Fresh flash reads 0xFF,
// which GetSaveValidStatus treats as "no save" -> the main menu shows NEW GAME
// rather than the SAVE_STATUS_NO_FLASH (gJPText_No1MSubCircuit) error dialog.
// Session-only for now; a .sav backing is the remaining Phase-5 work.

#define FLASH_1M_SECTOR_SIZE 0x1000
#define FLASH_1M_NUM_SECTORS 32

#define FLASH_1M_SIZE (FLASH_1M_SECTOR_SIZE * FLASH_1M_NUM_SECTORS)

static u8 sFlash[FLASH_1M_SIZE] = { [0 ... FLASH_1M_SIZE - 1] = 0xFF };
static int sFlashDirty;

// Optional battery .sav backing: PE_SAV=path makes the flash buffer persist, so
// a save written in one process (e.g. the GUI) is a checkpoint another (the
// headless driver) can CONTINUE from. Load runs before the game reads flash;
// writes are flushed once per frame from port_frame_end when dirty.
void pe_flash_init(void)
{
    const char *path = getenv("PE_SAV");
    if (!path)
        return;
    FILE *f = fopen(path, "rb");
    if (f)
    {
        fread(sFlash, 1, FLASH_1M_SIZE, f);
        fclose(f);
    }
}

void pe_flash_flush(void)
{
    const char *path;
    FILE *f;
    if (!sFlashDirty)
        return;
    sFlashDirty = 0;
    path = getenv("PE_SAV");
    if (!path)
        return;
    f = fopen(path, "wb");
    if (f)
    {
        fwrite(sFlash, 1, FLASH_1M_SIZE, f);
        fclose(f);
    }
}

static u16 HostEraseFlashSector(u16 sectorNum)
{
    if (sectorNum >= FLASH_1M_NUM_SECTORS)
        return 0x80;
    memset(&sFlash[sectorNum * FLASH_1M_SECTOR_SIZE], 0xFF, FLASH_1M_SECTOR_SIZE);
    sFlashDirty = 1;
    return 0;
}

static u16 HostProgramFlashByte(u16 sectorNum, u32 offset, u8 data)
{
    if (sectorNum >= FLASH_1M_NUM_SECTORS || offset >= FLASH_1M_SECTOR_SIZE)
        return 0x80;
    // Flash can only clear bits (1->0); the game erases before programming.
    sFlash[sectorNum * FLASH_1M_SECTOR_SIZE + offset] &= data;
    sFlashDirty = 1;
    return 0;
}

u16 (*EraseFlashSector)(u16) = HostEraseFlashSector;
u16 (*ProgramFlashByte)(u16, u32, u8) = HostProgramFlashByte;

void ReadFlash(u16 sectorNum, u32 offset, u8 *dest, u32 size)
{
    memcpy(dest, &sFlash[sectorNum * FLASH_1M_SECTOR_SIZE + offset], size);
}

u32 ProgramFlashSectorAndVerify(u16 sectorNum, u8 *src)
{
    u32 i;

    if (HostEraseFlashSector(sectorNum))
        return 1;
    for (i = 0; i < FLASH_1M_SECTOR_SIZE; i++)
    {
        if (HostProgramFlashByte(sectorNum, i, src[i]))
            return 1;
    }
    return memcmp(&sFlash[sectorNum * FLASH_1M_SECTOR_SIZE], src, FLASH_1M_SECTOR_SIZE) ? 1 : 0;
}

u16 IdentifyFlash(void)
{
    return 0;
}

u16 SetFlashTimerIntr(u8 timerNum, void (**intrFunc)(void))
{
    return 0;
}

#include "global.h"
#include "gba/gba.h"
#include "gba/m4a_internal.h"
#include "m4a.h"

// Sound is not synthesised on host, but the game reads gMPlayInfo_BGM.status to
// steer control flow: Task_TitleScreenPhase3 treats status==0 as "BGM ended,
// restart the attract loop", so with m4a fully stubbed (status permanently 0)
// the title screen self-reset every frame and gameplay was unreachable. This
// HLE tracks the one fact those readers need: a song counts as playing from
// start until it is explicitly stopped or faded (nothing ends on its own here,
// since MPlayMain never runs).

struct MusicPlayerInfo gMPlayInfo_BGM;
struct MusicPlayerInfo gMPlayInfo_SE1;
struct MusicPlayerInfo gMPlayInfo_SE2;
struct MusicPlayerInfo gMPlayInfo_SE3;

// gSongTable/gMPlayTable are emitted by port/tools/dataconv with GBA field
// packing plus pointers widened 4->8 bytes, so an entry is 8 bytes larger per
// pointer than the on-GBA struct but does NOT get the host compiler's natural
// re-padding: struct Song is 12 bytes here (vs sizeof 16) and struct MusicPlayer
// is 20 (vs sizeof 24). Indexing them as the native array types walks off the
// entries into neighbouring data, so read the packed fields by byte offset.
#define SONG_STRIDE   12
#define SONG_OFF_MS    8
#define MPLAY_STRIDE  20
#define MPLAY_OFF_INFO 0
#define MPLAY_COUNT    4

static struct MusicPlayerInfo *PlayerForSong(u16 n)
{
    const u8 *song = (const u8 *)gSongTable + (size_t)n * SONG_STRIDE;
    u16 ms;
    __builtin_memcpy(&ms, song + SONG_OFF_MS, sizeof ms);
    if (ms >= MPLAY_COUNT)
        return NULL;
    const u8 *mplay = (const u8 *)gMPlayTable + (size_t)ms * MPLAY_STRIDE;
    struct MusicPlayerInfo *info;
    __builtin_memcpy(&info, mplay + MPLAY_OFF_INFO, sizeof info);
    return info;
}

static struct SongHeader *HeaderForSong(u16 n)
{
    const u8 *song = (const u8 *)gSongTable + (size_t)n * SONG_STRIDE;
    struct SongHeader *header;
    __builtin_memcpy(&header, song, sizeof header);
    return header;
}

static void StartSong(u16 n)
{
    struct MusicPlayerInfo *info = PlayerForSong(n);
    if (info == NULL)
        return;
    info->songHeader = HeaderForSong(n);
    info->status = MUSICPLAYER_STATUS_TRACK;
}

void m4aSongNumStart(u16 n)
{
    StartSong(n);
}

void m4aSongNumStartOrChange(u16 n)
{
    StartSong(n);
}

void m4aSongNumStop(u16 n)
{
    struct MusicPlayerInfo *info = PlayerForSong(n);
    if (info != NULL && info->songHeader == HeaderForSong(n))
        info->status = 0;
}

void m4aMPlayStop(struct MusicPlayerInfo *mplayInfo)
{
    if (mplayInfo != NULL)
        mplayInfo->status = 0;
}

void m4aMPlayContinue(struct MusicPlayerInfo *mplayInfo)
{
    if (mplayInfo != NULL)
        mplayInfo->status = MUSICPLAYER_STATUS_TRACK;
}

void m4aMPlayFadeIn(struct MusicPlayerInfo *mplayInfo, u16 speed)
{
    if (mplayInfo != NULL)
        mplayInfo->status = MUSICPLAYER_STATUS_TRACK;
}

void m4aMPlayFadeOut(struct MusicPlayerInfo *mplayInfo, u16 speed)
{
    if (mplayInfo != NULL)
        mplayInfo->status = 0;
}

void m4aMPlayFadeOutTemporarily(struct MusicPlayerInfo *mplayInfo, u16 speed)
{
    if (mplayInfo != NULL)
        mplayInfo->status = 0;
}

void m4aMPlayAllStop(void)
{
    gMPlayInfo_BGM.status = 0;
    gMPlayInfo_SE1.status = 0;
    gMPlayInfo_SE2.status = 0;
    gMPlayInfo_SE3.status = 0;
}

// Cries never synthesise on host; if left to the garbage-returning genstub,
// IsPokemonCryPlaying can read non-zero and hang the battle intro's cry-duck
// wait (Task_DuckBGMForPokemonCry) forever. Report cries as always finished.
bool32 IsPokemonCryPlaying(struct MusicPlayerInfo *mplayInfo)
{
    (void)mplayInfo;
    return FALSE;
}

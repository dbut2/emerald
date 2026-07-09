#ifndef GUARD_ALLOC_H
#define GUARD_ALLOC_H


#define FREE_AND_SET_NULL(ptr)          \
{                                       \
    Free(ptr);                          \
    ptr = NULL;                         \
}

#define TRY_FREE_AND_SET_NULL(ptr) if (ptr != NULL) FREE_AND_SET_NULL(ptr)

// Host: LP64 makes every heap-allocated struct's pointer fields 8 bytes, so the
// battle's allocations overflow the GBA-sized 0x1C000 heap (AllocZeroed then
// returns NULL and callers crash). Host RAM is abundant; enlarge the arena.
#define HEAP_SIZE 0x100000
extern u8 gHeap[HEAP_SIZE];

void *Alloc(u32 size);
void *AllocZeroed(u32 size);
void Free(void *pointer);
void InitHeap(void *heapStart, u32 heapSize);

#endif // GUARD_ALLOC_H

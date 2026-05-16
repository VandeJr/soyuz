// Soyuz Standard Library — Collections runtime (FFI via extern fn)
// Linked by the Soyuz compiler alongside rc.c and std_*.c.
#include <stdint.h>
#include "soyuz.h"

// Builds a List[Int] with integers in [from, to).
void *soyuz_range(int64_t from, int64_t to) {
    int64_t count = to > from ? to - from : 0;
    void *list = soyuz_list_new(count, soyuz_list_dtor_primitive);
    for (int64_t i = from; i < to; i++) {
        soyuz_list_append(list, (void *)(uintptr_t)i);
    }
    return list;
}

// Builds a List[Int] with integers in [from, to] (inclusive).
void *soyuz_range_inclusive(int64_t from, int64_t to) {
    int64_t count = to >= from ? to - from + 1 : 0;
    void *list = soyuz_list_new(count, soyuz_list_dtor_primitive);
    for (int64_t i = from; i <= to; i++) {
        soyuz_list_append(list, (void *)(uintptr_t)i);
    }
    return list;
}

// Builds a List[Int] with integers from, from+step, from+2*step, ...
// stopping before reaching to (exclusive). step must be non-zero.
void *soyuz_range_step(int64_t from, int64_t to, int64_t step) {
    if (step == 0) return soyuz_list_new(0, soyuz_list_dtor_primitive);
    int64_t capacity = 8;
    void *list = soyuz_list_new(capacity, soyuz_list_dtor_primitive);
    if (step > 0) {
        for (int64_t i = from; i < to; i += step) {
            soyuz_list_append(list, (void *)(uintptr_t)i);
        }
    } else {
        for (int64_t i = from; i > to; i += step) {
            soyuz_list_append(list, (void *)(uintptr_t)i);
        }
    }
    return list;
}

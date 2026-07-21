// Soyuz Standard Library — Collections runtime (FFI via extern fn)
// Linked by the Soyuz compiler alongside rc.c and std_*.c.
#include <stdio.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
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

static SoyuzString *str_append_cstr(SoyuzString *acc, const char *piece) {
    SoyuzString *p = soyuz_str_from_cstr(piece);
    SoyuzString *next = soyuz_str_concat(acc, p);
    return next;
}

// elem_kind: 0 = Int (stored as uintptr in list slots), 1 = String (SoyuzString*)
SoyuzString *soyuz_list_to_string(void *list_ptr, int64_t elem_kind) {
    int64_t n = soyuz_list_size(list_ptr);
    SoyuzString *out = soyuz_str_from_cstr("[");
    for (int64_t i = 0; i < n; i++) {
        if (i > 0) {
            out = str_append_cstr(out, ", ");
        }
        void *slot = soyuz_list_get(list_ptr, i);
        SoyuzString *elem = NULL;
        if (elem_kind == 1) {
            elem = (SoyuzString *)slot;
            if (!elem) {
                elem = soyuz_str_from_cstr("");
            }
        } else {
            int64_t val = (int64_t)(uintptr_t)slot;
            elem = soyuz_int_to_str(val);
        }
        out = soyuz_str_concat(out, elem);
    }
    return str_append_cstr(out, "]");
}

// key_is_string: map uses string keys; val_kind: 0 = Int (uintptr), 1 = String (SoyuzString*)
SoyuzString *soyuz_map_to_string(void *map_ptr, int64_t key_is_string, int64_t val_kind) {
    void *keys = soyuz_map_keys(map_ptr, key_is_string);
    int64_t n = soyuz_list_size(keys);
    SoyuzString *out = soyuz_str_from_cstr("{");
    for (int64_t i = 0; i < n; i++) {
        if (i > 0) {
            out = str_append_cstr(out, ", ");
        }
        void *raw_key = soyuz_list_get(keys, i);
        SoyuzString *keyStr = NULL;
        if (key_is_string) {
            keyStr = (SoyuzString *)raw_key;
            if (!keyStr) keyStr = soyuz_str_from_cstr("");
        } else {
            keyStr = soyuz_int_to_str((int64_t)(uintptr_t)raw_key);
        }
        out = soyuz_str_concat(out, keyStr);
        out = str_append_cstr(out, ": ");
        void *val = soyuz_map_get(map_ptr, raw_key);
        SoyuzString *valStr = NULL;
        if (val_kind == 1) {
            valStr = (SoyuzString *)val;
            if (!valStr) valStr = soyuz_str_from_cstr("");
        } else {
            valStr = soyuz_int_to_str((int64_t)(uintptr_t)val);
        }
        out = soyuz_str_concat(out, valStr);
    }
    return str_append_cstr(out, "}");
}

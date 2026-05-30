#pragma once
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef void (*SoyuzDtor)(void *);
typedef void (*SoyuzTraceFn)(void *ptr, void (*visit)(void *child));

#define SOYUZ_STATIC_REFCOUNT INT64_MAX

#define SOYUZ_ORC_REGISTERED 1
#define SOYUZ_ORC_DEAD       2
#define SOYUZ_ORC_FREED      4
#define SOYUZ_ORC_COLOR_WHITE  0
#define SOYUZ_ORC_COLOR_GRAY   1
#define SOYUZ_ORC_COLOR_BLACK  2

typedef struct SoyuzHeader {
    int64_t     refcount;
    SoyuzDtor   dtor;
    SoyuzTraceFn trace;
    uint8_t     color;
    uint8_t     flags;
    struct SoyuzHeader *next_orc;
} SoyuzHeader;

typedef struct {
    int64_t len;
} SoyuzString;

static inline const char *soyuz_str_data(const SoyuzString *s) {
    return s ? (const char *)(s + 1) : "";
}

void *soyuz_alloc(int64_t size, SoyuzDtor dtor, SoyuzTraceFn trace);
void  soyuz_retain(void *ptr);
void  soyuz_release(void *ptr);
void  soyuz_orc_collect(void);

SoyuzString *soyuz_str_new(const char *data, int64_t len);
SoyuzString *soyuz_str_from_cstr(const char *cstr);
SoyuzString *soyuz_str_from_printf_buf(char *buf);
int64_t soyuz_str_len(SoyuzString *s);
int64_t soyuz_str_is_empty(SoyuzString *s);
SoyuzString *soyuz_str_concat(SoyuzString *s1, SoyuzString *s2);
SoyuzString *soyuz_str_trim(SoyuzString *s);
SoyuzString *soyuz_str_to_upper(SoyuzString *s);
SoyuzString *soyuz_str_to_lower(SoyuzString *s);
SoyuzString *soyuz_str_substring(SoyuzString *s, int64_t start, int64_t end);
SoyuzString *soyuz_str_replace(SoyuzString *s, SoyuzString *from, SoyuzString *to);
int64_t soyuz_str_contains(SoyuzString *s, SoyuzString *sub);
int64_t soyuz_str_starts_with(SoyuzString *s, SoyuzString *prefix);
int64_t soyuz_str_ends_with(SoyuzString *s, SoyuzString *suffix);
int64_t soyuz_str_index_of(SoyuzString *s, SoyuzString *sub);
int64_t soyuz_str_last_index_of(SoyuzString *s, SoyuzString *sub);
int64_t soyuz_str_byte_at(SoyuzString *s, int64_t index);
int64_t soyuz_str_unicode_at(SoyuzString *s, int64_t char_index);
void *soyuz_str_split(SoyuzString *s, SoyuzString *delim);

void *soyuz_list_new(int64_t initial_capacity, SoyuzDtor dtor);
void  soyuz_list_append(void *list_ptr, void *value);
void *soyuz_list_get(void *list_ptr, int64_t index);
void  soyuz_list_dtor_rc(void *ptr);
void  soyuz_list_dtor_primitive(void *ptr);
void  soyuz_list_set(void *list_ptr, int64_t index, void *value);
void  soyuz_list_set_rc(void *list_ptr, int64_t index, void *value);
void *soyuz_list_remove(void *list_ptr, int64_t index);
void *soyuz_list_pop(void *list_ptr);
void  soyuz_list_prepend(void *list_ptr, void *value);
void  soyuz_list_clear_rc(void *list_ptr);
void  soyuz_list_clear_primitive(void *list_ptr);
void *soyuz_list_copy(void *list_ptr, int64_t elem_is_heap);
void *soyuz_list_concat(void *list_a, void *list_b, int64_t elem_is_heap);

void *soyuz_map_new(int64_t is_string_key, SoyuzDtor dtor);
void  soyuz_map_set(void *map_ptr, void *key, void *value);
void *soyuz_map_get(void *map_ptr, void *key);
void  soyuz_map_dtor_primitive(void *ptr);
void  soyuz_map_dtor_rc_key(void *ptr);
void  soyuz_map_dtor_rc_val(void *ptr);
void  soyuz_map_dtor_rc_both(void *ptr);
void *soyuz_map_keys(void *map_ptr, int64_t key_is_heap);
void *soyuz_map_values(void *map_ptr, int64_t val_is_heap);

int64_t soyuz_int_to_str_len(int64_t n);
SoyuzString *soyuz_int_to_str(int64_t n);
int64_t soyuz_int_abs(int64_t n);
double soyuz_int_to_float(int64_t n);

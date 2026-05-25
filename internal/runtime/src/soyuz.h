#pragma once
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef void (*SoyuzDtor)(void *);

typedef struct {
    int64_t   refcount;
    SoyuzDtor dtor;
} SoyuzHeader;

/* SOYUZ_STATIC_REFCOUNT: sentinel usado em string literals estáticas para
   que soyuz_retain/soyuz_release sejam no-ops (nunca liberar memória estática). */
#define SOYUZ_STATIC_REFCOUNT INT64_MAX

/* SoyuzString — string reference-counted.
   Os bytes null-terminated ficam inline imediatamente após esta struct.
   Use soyuz_str_data(s) para acessar o char*. */
typedef struct {
    int64_t len;
} SoyuzString;

static inline const char *soyuz_str_data(const SoyuzString *s) {
    return s ? (const char *)(s + 1) : "";
}

/* Declarações das funções de RC (definidas em rc.c) */
void *soyuz_alloc(int64_t size, SoyuzDtor dtor);
void  soyuz_retain(void *ptr);
void  soyuz_release(void *ptr);

SoyuzString *soyuz_str_new(const char *data, int64_t len);
SoyuzString *soyuz_str_from_cstr(const char *cstr);
/* Cria SoyuzString de um buffer malloc'd do sprintf e libera o buffer. */
SoyuzString *soyuz_str_from_printf_buf(char *buf);

/* List primitives (definidos em rc.c) */
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

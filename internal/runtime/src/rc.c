#include "soyuz.h"

#define SOYUZ_ORC_COLLECT_THRESHOLD 256

static SoyuzHeader *orc_registry = NULL;
static int64_t orc_release_counter = 0;

static void orc_register(SoyuzHeader *h) {
    if (!h || !h->trace || (h->flags & SOYUZ_ORC_REGISTERED)) return;
    h->flags |= SOYUZ_ORC_REGISTERED;
    h->next_orc = orc_registry;
    orc_registry = h;
}

static void orc_unregister(SoyuzHeader *h) {
    if (!h || !(h->flags & SOYUZ_ORC_REGISTERED)) return;
    SoyuzHeader **pp = &orc_registry;
    while (*pp) {
        if (*pp == h) {
            *pp = h->next_orc;
            h->next_orc = NULL;
            h->flags &= ~SOYUZ_ORC_REGISTERED;
            return;
        }
        pp = &(*pp)->next_orc;
    }
}

typedef struct {
    SoyuzHeader *header;
    int64_t trial;
    int alive;
} OrcEntry;

static OrcEntry *orc_entries = NULL;
static int orc_entry_cap = 0;
static int orc_entry_count = 0;

static int orc_entry_index(SoyuzHeader *h) {
    for (int i = 0; i < orc_entry_count; i++) {
        if (orc_entries[i].header == h && orc_entries[i].alive) {
            return i;
        }
    }
    return -1;
}

static void orc_trial_visit(void *child) {
    if (!child) return;
    SoyuzHeader *ch = (SoyuzHeader *)child - 1;
    int idx = orc_entry_index(ch);
    if (idx >= 0 && orc_entries[idx].trial > 0) {
        orc_entries[idx].trial--;
    }
}

static void orc_free_header(SoyuzHeader *h) {
    if (h->flags & SOYUZ_ORC_FREED) return;
    h->flags |= SOYUZ_ORC_DEAD | SOYUZ_ORC_FREED;
    void *ptr = h + 1;
    orc_unregister(h);
    if (h->dtor) h->dtor(ptr);
    free(h);
}

void soyuz_orc_collect(void) {
    orc_entry_count = 0;
    for (SoyuzHeader *h = orc_registry; h; h = h->next_orc) {
        if (h->refcount > 0 && h->trace) {
            if (orc_entry_count >= orc_entry_cap) {
                int new_cap = orc_entry_cap == 0 ? 64 : orc_entry_cap * 2;
                OrcEntry *next = (OrcEntry *)realloc(orc_entries, (size_t)new_cap * sizeof(OrcEntry));
                if (!next) return;
                orc_entries = next;
                orc_entry_cap = new_cap;
            }
            orc_entries[orc_entry_count].header = h;
            orc_entries[orc_entry_count].trial = h->refcount;
            orc_entries[orc_entry_count].alive = 1;
            orc_entry_count++;
        }
    }

    int changed = 1;
    while (changed) {
        changed = 0;
        for (int i = 0; i < orc_entry_count; i++) {
            if (!orc_entries[i].alive) continue;
            orc_entries[i].trial = orc_entries[i].header->refcount;
        }
        for (int i = 0; i < orc_entry_count; i++) {
            if (!orc_entries[i].alive) continue;
            SoyuzHeader *h = orc_entries[i].header;
            if (h->trace) {
                h->trace(h + 1, orc_trial_visit);
            }
        }
        for (int i = 0; i < orc_entry_count; i++) {
            if (!orc_entries[i].alive) continue;
            if (orc_entries[i].trial == 0 && orc_entries[i].header->refcount > 0) {
                orc_entries[i].header->flags |= SOYUZ_ORC_DEAD;
            }
        }
        for (int i = 0; i < orc_entry_count; i++) {
            if (!orc_entries[i].alive) continue;
            if (orc_entries[i].header->flags & SOYUZ_ORC_DEAD) {
                orc_free_header(orc_entries[i].header);
                orc_entries[i].alive = 0;
                changed = 1;
            }
        }
    }
}

void *soyuz_alloc(int64_t size, SoyuzDtor dtor, SoyuzTraceFn trace) {
    SoyuzHeader *h = (SoyuzHeader *)malloc(sizeof(SoyuzHeader) + (size_t)size);
    if (!h) return NULL;
    h->refcount = 1;
    h->dtor = dtor;
    h->trace = trace;
    h->color = SOYUZ_ORC_COLOR_WHITE;
    h->flags = 0;
    h->next_orc = NULL;
    if (dtor && trace) {
        orc_register(h);
    }
    return h + 1;
}

void soyuz_retain(void *ptr) {
    if (!ptr) return;
    SoyuzHeader *h = (SoyuzHeader *)ptr - 1;
    if (h->refcount == SOYUZ_STATIC_REFCOUNT || (h->flags & SOYUZ_ORC_DEAD)) return;
    h->refcount++;
}

void soyuz_release(void *ptr) {
    if (!ptr) return;
    SoyuzHeader *h = (SoyuzHeader *)ptr - 1;
    if (h->refcount == SOYUZ_STATIC_REFCOUNT || (h->flags & SOYUZ_ORC_DEAD)) return;
    if (--h->refcount == 0) {
        orc_unregister(h);
        if (h->dtor) h->dtor(ptr);
        free(h);
        return;
    }
    if (h->trace && ++orc_release_counter >= SOYUZ_ORC_COLLECT_THRESHOLD) {
        orc_release_counter = 0;
        soyuz_orc_collect();
    }
}

typedef struct {
    int64_t size;
    int64_t capacity;
    void **data;
} SoyuzList;

void *soyuz_list_new(int64_t initial_capacity, SoyuzDtor dtor) {
    SoyuzList *list = (SoyuzList *)soyuz_alloc(sizeof(SoyuzList), dtor, NULL);
    list->size = 0;
    list->capacity = initial_capacity;
    if (initial_capacity > 0) {
        list->data = (void **)malloc(sizeof(void *) * (size_t)initial_capacity);
    } else {
        list->data = NULL;
    }
    return list;
}

void soyuz_list_append(void *list_ptr, void *value) {
    SoyuzList *list = (SoyuzList *)list_ptr;
    if (list->size >= list->capacity) {
        list->capacity = list->capacity == 0 ? 4 : list->capacity * 2;
        list->data = (void **)realloc(list->data, sizeof(void *) * (size_t)list->capacity);
    }
    list->data[list->size++] = value;
}

void *soyuz_list_get(void *list_ptr, int64_t index) {
    SoyuzList *list = (SoyuzList *)list_ptr;
    if (index < 0 || index >= list->size) return NULL;
    return list->data[index];
}

int64_t soyuz_list_size(void *list_ptr) {
    SoyuzList *list = (SoyuzList *)list_ptr;
    return list ? list->size : 0;
}

void soyuz_list_dtor_rc(void *ptr) {
    SoyuzList *list = (SoyuzList *)ptr;
    for (int64_t i = 0; i < list->size; i++) {
        soyuz_release(list->data[i]);
    }
    free(list->data);
}

void soyuz_list_dtor_primitive(void *ptr) {
    SoyuzList *list = (SoyuzList *)ptr;
    free(list->data);
}

void soyuz_list_set(void *list_ptr, int64_t index, void *value) {
    SoyuzList *list = (SoyuzList *)list_ptr;
    if (index < 0 || index >= list->size) return;
    list->data[index] = value;
}

void soyuz_list_set_rc(void *list_ptr, int64_t index, void *value) {
    SoyuzList *list = (SoyuzList *)list_ptr;
    if (index < 0 || index >= list->size) return;
    soyuz_release(list->data[index]);
    list->data[index] = value;
}

void *soyuz_list_remove(void *list_ptr, int64_t index) {
    SoyuzList *list = (SoyuzList *)list_ptr;
    if (index < 0 || index >= list->size) return NULL;
    void *removed = list->data[index];
    for (int64_t i = index; i < list->size - 1; i++) {
        list->data[i] = list->data[i + 1];
    }
    list->size--;
    return removed;
}

void *soyuz_list_pop(void *list_ptr) {
    SoyuzList *list = (SoyuzList *)list_ptr;
    if (list->size == 0) return NULL;
    return list->data[--list->size];
}

void soyuz_list_prepend(void *list_ptr, void *value) {
    SoyuzList *list = (SoyuzList *)list_ptr;
    if (list->size >= list->capacity) {
        list->capacity = list->capacity == 0 ? 4 : list->capacity * 2;
        list->data = (void **)realloc(list->data, sizeof(void *) * (size_t)list->capacity);
    }
    memmove(list->data + 1, list->data, sizeof(void *) * (size_t)list->size);
    list->data[0] = value;
    list->size++;
}

void soyuz_list_clear_rc(void *list_ptr) {
    SoyuzList *list = (SoyuzList *)list_ptr;
    for (int64_t i = 0; i < list->size; i++) {
        soyuz_release(list->data[i]);
    }
    list->size = 0;
}

void soyuz_list_clear_primitive(void *list_ptr) {
    SoyuzList *list = (SoyuzList *)list_ptr;
    list->size = 0;
}

void *soyuz_list_copy(void *list_ptr, int64_t elem_is_heap) {
    SoyuzList *list = (SoyuzList *)list_ptr;
    SoyuzDtor dtor = elem_is_heap ? soyuz_list_dtor_rc : soyuz_list_dtor_primitive;
    void *result = soyuz_list_new(list->size, dtor);
    for (int64_t i = 0; i < list->size; i++) {
        soyuz_list_append(result, list->data[i]);
        if (elem_is_heap) soyuz_retain(list->data[i]);
    }
    return result;
}

void *soyuz_list_concat(void *list_a, void *list_b, int64_t elem_is_heap) {
    SoyuzList *a = (SoyuzList *)list_a;
    SoyuzList *b = (SoyuzList *)list_b;
    SoyuzDtor dtor = elem_is_heap ? soyuz_list_dtor_rc : soyuz_list_dtor_primitive;
    void *result = soyuz_list_new(a->size + b->size, dtor);
    for (int64_t i = 0; i < a->size; i++) {
        soyuz_list_append(result, a->data[i]);
        if (elem_is_heap) soyuz_retain(a->data[i]);
    }
    for (int64_t i = 0; i < b->size; i++) {
        soyuz_list_append(result, b->data[i]);
        if (elem_is_heap) soyuz_retain(b->data[i]);
    }
    return result;
}

typedef struct {
    void *key;
    void *value;
    int64_t occupied;
} SoyuzMapEntry;

typedef struct {
    int64_t size;
    int64_t capacity;
    SoyuzMapEntry *entries;
    int64_t is_string_key;
} SoyuzMap;

static uint64_t soyuz_hash(void *key, int64_t is_string) {
    if (is_string) {
        uint64_t hash = 5381;
        const char *str = soyuz_str_data((const SoyuzString *)key);
        int c;
        while ((c = *str++)) hash = ((hash << 5) + hash) + c;
        return hash;
    }
    return (uint64_t)key;
}

static int soyuz_key_eq(void *k1, void *k2, int64_t is_string) {
    if (is_string) {
        if (k1 == k2) return 1;
        if (!k1 || !k2) return 0;
        return strcmp(soyuz_str_data((const SoyuzString *)k1), 
                      soyuz_str_data((const SoyuzString *)k2)) == 0;
    }
    return k1 == k2;
}

void *soyuz_map_new(int64_t is_string_key, SoyuzDtor dtor) {
    SoyuzMap *map = (SoyuzMap *)soyuz_alloc(sizeof(SoyuzMap), dtor, NULL);
    map->size = 0;
    map->capacity = 16;
    map->entries = (SoyuzMapEntry *)calloc(map->capacity, sizeof(SoyuzMapEntry));
    map->is_string_key = is_string_key;
    return map;
}

void soyuz_map_set(void *map_ptr, void *key, void *value) {
    SoyuzMap *map = (SoyuzMap *)map_ptr;
    if (map->size * 2 >= map->capacity) {
        // Resize
        int64_t old_cap = map->capacity;
        SoyuzMapEntry *old_entries = map->entries;
        map->capacity *= 2;
        map->entries = (SoyuzMapEntry *)calloc(map->capacity, sizeof(SoyuzMapEntry));
        map->size = 0;
        for (int64_t i = 0; i < old_cap; i++) {
            if (old_entries[i].occupied) {
                soyuz_map_set(map, old_entries[i].key, old_entries[i].value);
            }
        }
        free(old_entries);
    }

    uint64_t h = soyuz_hash(key, map->is_string_key);
    int64_t idx = h % map->capacity;
    while (map->entries[idx].occupied) {
        if (soyuz_key_eq(map->entries[idx].key, key, map->is_string_key)) {
            if (map->entries[idx].value != value) {
                soyuz_release(map->entries[idx].value);
            }
            map->entries[idx].value = value;
            return;
        }
        idx = (idx + 1) % map->capacity;
    }
    map->entries[idx].key = key;
    map->entries[idx].value = value;
    map->entries[idx].occupied = 1;
    map->size++;
}

int64_t soyuz_map_size(void *map_ptr) {
    SoyuzMap *map = (SoyuzMap *)map_ptr;
    return map ? map->size : 0;
}

void *soyuz_map_get(void *map_ptr, void *key) {
    SoyuzMap *map = (SoyuzMap *)map_ptr;
    uint64_t h = soyuz_hash(key, map->is_string_key);
    int64_t idx = h % map->capacity;
    while (map->entries[idx].occupied) {
        if (soyuz_key_eq(map->entries[idx].key, key, map->is_string_key)) {
            return map->entries[idx].value;
        }
        idx = (idx + 1) % map->capacity;
    }
    return NULL;
}

void soyuz_map_dtor_primitive(void *ptr) {
    SoyuzMap *map = (SoyuzMap *)ptr;
    free(map->entries);
}

void soyuz_map_dtor_rc_key(void *ptr) {
    SoyuzMap *map = (SoyuzMap *)ptr;
    for (int64_t i = 0; i < map->capacity; i++) {
        if (map->entries[i].occupied) {
            soyuz_release(map->entries[i].key);
        }
    }
    free(map->entries);
}

void soyuz_map_dtor_rc_val(void *ptr) {
    SoyuzMap *map = (SoyuzMap *)ptr;
    for (int64_t i = 0; i < map->capacity; i++) {
        if (map->entries[i].occupied) {
            soyuz_release(map->entries[i].value);
        }
    }
    free(map->entries);
}

void soyuz_map_dtor_rc_both(void *ptr) {
    SoyuzMap *map = (SoyuzMap *)ptr;
    for (int64_t i = 0; i < map->capacity; i++) {
        if (map->entries[i].occupied) {
            soyuz_release(map->entries[i].key);
            soyuz_release(map->entries[i].value);
        }
    }
    free(map->entries);
}

void *soyuz_map_keys(void *map_ptr, int64_t key_is_heap) {
    SoyuzMap *map = (SoyuzMap *)map_ptr;
    SoyuzDtor dtor = key_is_heap ? soyuz_list_dtor_rc : soyuz_list_dtor_primitive;
    void *result = soyuz_list_new(map->size, dtor);
    for (int64_t i = 0; i < map->capacity; i++) {
        if (map->entries[i].occupied) {
            soyuz_list_append(result, map->entries[i].key);
            if (key_is_heap) soyuz_retain(map->entries[i].key);
        }
    }
    return result;
}

void *soyuz_map_values(void *map_ptr, int64_t val_is_heap) {
    SoyuzMap *map = (SoyuzMap *)map_ptr;
    SoyuzDtor dtor = val_is_heap ? soyuz_list_dtor_rc : soyuz_list_dtor_primitive;
    void *result = soyuz_list_new(map->size, dtor);
    for (int64_t i = 0; i < map->capacity; i++) {
        if (map->entries[i].occupied) {
            soyuz_list_append(result, map->entries[i].value);
            if (val_is_heap) soyuz_retain(map->entries[i].value);
        }
    }
    return result;
}

SoyuzString *soyuz_str_new(const char *data, int64_t len) {
    SoyuzString *s = (SoyuzString *)soyuz_alloc(
        (int64_t)(sizeof(SoyuzString) + (size_t)len + 1), NULL, NULL);
    if (!s) return NULL;
    s->len = len;
    char *dest = (char *)(s + 1);
    memcpy(dest, data, (size_t)len);
    dest[len] = '\0';
    return s;
}

SoyuzString *soyuz_str_from_cstr(const char *cstr) {
    if (!cstr) return soyuz_str_new("", 0);
    return soyuz_str_new(cstr, (int64_t)strlen(cstr));
}

SoyuzString *soyuz_str_from_printf_buf(char *buf) {
    if (!buf) return soyuz_str_new("", 0);
    int64_t len = (int64_t)strlen(buf);
    SoyuzString *s = soyuz_str_new(buf, len);
    free(buf);
    return s;
}

int64_t soyuz_str_len(SoyuzString *s) {
    return s ? s->len : 0;
}

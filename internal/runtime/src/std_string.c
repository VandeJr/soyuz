// Soyuz Standard Library — String operations (FFI via extern fn)
// Linked by the Soyuz compiler alongside rc.c and std_io.c.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <ctype.h>
#include "soyuz.h"

// Thread-local parse error state for safe toInt and toFloat conversions
_Thread_local static int64_t parse_err = 0;

int64_t soyuz_str_parse_has_error(void) {
    return parse_err;
}

// Retorna uma nova substring [start, end). Restringe os índices à margem válida.
SoyuzString *soyuz_str_substring(SoyuzString *s, int64_t start, int64_t end) {
    if (!s) return soyuz_str_new("", 0);
    const char *data = soyuz_str_data(s);
    int64_t len = s->len;
    if (start < 0) start = 0;
    if (end > len) end = len;
    if (start >= end) return soyuz_str_new("", 0);
    return soyuz_str_new(data + start, end - start);
}

// Acesso O(1) rápido ao byte — Devolve -1 se fora dos limites.
int64_t soyuz_str_byte_at(SoyuzString *s, int64_t index) {
    if (!s) return -1;
    if (index < 0 || index >= s->len) return -1;
    return (int64_t)(unsigned char)soyuz_str_data(s)[index];
}

// Acesso O(N) que decodifica UTF-8 para extrair o Code Point lógico (Rune).
int64_t soyuz_str_unicode_at(SoyuzString *s, int64_t char_index) {
    if (!s || char_index < 0) return -1;
    const unsigned char *p = (const unsigned char*)soyuz_str_data(s);
    int64_t current_char = 0;
    
    while (*p) {
        int32_t codepoint = 0;
        int bytes = 0;
        
        if (*p < 0x80) {
            codepoint = *p;
            bytes = 1;
        } else if ((*p & 0xE0) == 0xC0) {
            codepoint = *p & 0x1F;
            bytes = 2;
        } else if ((*p & 0xF0) == 0xE0) {
            codepoint = *p & 0x0F;
            bytes = 3;
        } else if ((*p & 0xF8) == 0xF0) {
            codepoint = *p & 0x07;
            bytes = 4;
        } else {
            // Byte inválido
            codepoint = -1;
            bytes = 1;
        }
        
        // Acumulação de bytes de continuação do UTF-8
        for (int i = 1; i < bytes; i++) {
            if (p[i] == '\0' || (p[i] & 0xC0) != 0x80) {
                codepoint = -1;
                break;
            }
            codepoint = (codepoint << 6) | (p[i] & 0x3F);
        }
        
        if (current_char == char_index) {
            return (int64_t)codepoint;
        }
        
        p += bytes;
        current_char++;
    }
    return -1;
}

// Converte o retorno int64 do C para o primitivo i32 Char usado no LLVM do Soyuz.
int32_t soyuz_int_to_char(int64_t i) {
    return (int32_t)i;
}

SoyuzString *soyuz_str_concat(SoyuzString *s1, SoyuzString *s2) {
    const char *d1 = s1 ? soyuz_str_data(s1) : "";
    const char *d2 = s2 ? soyuz_str_data(s2) : "";
    int64_t l1 = s1 ? s1->len : 0;
    int64_t l2 = s2 ? s2->len : 0;
    int64_t newLen = l1 + l2;
    char *buf = (char *)malloc((size_t)(newLen + 1));
    if (!buf) return soyuz_str_new("", 0);
    memcpy(buf, d1, (size_t)l1);
    memcpy(buf + l1, d2, (size_t)l2);
    buf[newLen] = '\0';
    SoyuzString *result = soyuz_str_new(buf, newLen);
    free(buf);
    return result;
}

SoyuzString *soyuz_str_trim(SoyuzString *s) {
    if (!s) return soyuz_str_new("", 0);
    const char *data = soyuz_str_data(s);
    int64_t len = s->len;
    int64_t start = 0;
    while (start < len && isspace((unsigned char)data[start])) start++;
    int64_t end = len;
    while (end > start && isspace((unsigned char)data[end - 1])) end--;
    return soyuz_str_new(data + start, end - start);
}

SoyuzString *soyuz_str_to_upper(SoyuzString *s) {
    if (!s) return soyuz_str_new("", 0);
    const char *data = soyuz_str_data(s);
    int64_t len = s->len;
    char *buf = (char *)malloc((size_t)(len + 1));
    if (!buf) return soyuz_str_new("", 0);
    for (int64_t i = 0; i < len; i++) buf[i] = (char)toupper((unsigned char)data[i]);
    buf[len] = '\0';
    SoyuzString *result = soyuz_str_new(buf, len);
    free(buf);
    return result;
}

SoyuzString *soyuz_str_to_lower(SoyuzString *s) {
    if (!s) return soyuz_str_new("", 0);
    const char *data = soyuz_str_data(s);
    int64_t len = s->len;
    char *buf = (char *)malloc((size_t)(len + 1));
    if (!buf) return soyuz_str_new("", 0);
    for (int64_t i = 0; i < len; i++) buf[i] = (char)tolower((unsigned char)data[i]);
    buf[len] = '\0';
    SoyuzString *result = soyuz_str_new(buf, len);
    free(buf);
    return result;
}

int64_t soyuz_str_contains(SoyuzString *s, SoyuzString *sub) {
    if (!s || !sub) return 0;
    return strstr(soyuz_str_data(s), soyuz_str_data(sub)) != NULL ? 1 : 0;
}

int64_t soyuz_str_starts_with(SoyuzString *s, SoyuzString *prefix) {
    if (!s || !prefix) return 0;
    return strncmp(soyuz_str_data(s), soyuz_str_data(prefix), (size_t)prefix->len) == 0 ? 1 : 0;
}

int64_t soyuz_str_ends_with(SoyuzString *s, SoyuzString *suffix) {
    if (!s || !suffix) return 0;
    int64_t sl = s->len, pl = suffix->len;
    if (pl > sl) return 0;
    return strcmp(soyuz_str_data(s) + sl - pl, soyuz_str_data(suffix)) == 0 ? 1 : 0;
}

int64_t soyuz_str_index_of(SoyuzString *s, SoyuzString *sub) {
    if (!s || !sub) return -1;
    const char *found = strstr(soyuz_str_data(s), soyuz_str_data(sub));
    return found ? (int64_t)(found - soyuz_str_data(s)) : -1;
}

int64_t soyuz_str_last_index_of(SoyuzString *s, SoyuzString *sub) {
    if (!s || !sub || sub->len == 0) return -1;
    const char *data = soyuz_str_data(s);
    const char *subdata = soyuz_str_data(sub);
    int64_t slen = s->len, sublen = sub->len;
    int64_t result = -1;
    const char *p = data;
    const char *found;
    while ((found = strstr(p, subdata)) != NULL) {
        result = (int64_t)(found - data);
        p = found + sublen;
        if (p >= data + slen) break;
    }
    return result;
}

SoyuzString *soyuz_str_replace(SoyuzString *s, SoyuzString *from, SoyuzString *to) {
    if (!s || !from || !to) return s ? s : soyuz_str_new("", 0);
    const char *sdata = soyuz_str_data(s);
    const char *fdata = soyuz_str_data(from);
    const char *tdata = soyuz_str_data(to);
    int64_t fromLen = from->len;
    if (fromLen == 0) return soyuz_str_new(sdata, s->len);
    
    int64_t toLen = to->len;
    int64_t count = 0;
    const char *p = sdata;
    while ((p = strstr(p, fdata)) != NULL) { count++; p += fromLen; }
    
    int64_t newLen = s->len + count * (toLen - fromLen);
    char *buf = (char *)malloc((size_t)(newLen + 1));
    if (!buf) return soyuz_str_new("", 0);
    
    char *out = buf;
    p = sdata;
    const char *next;
    while ((next = strstr(p, fdata)) != NULL) {
        size_t chunk = (size_t)(next - p);
        memcpy(out, p, chunk);
        out += chunk;
        memcpy(out, tdata, (size_t)toLen);
        out += toLen;
        p = next + fromLen;
    }
    strcpy(out, p);
    SoyuzString *result = soyuz_str_new(buf, newLen);
    free(buf);
    return result;
}

void* soyuz_str_split(SoyuzString *s, SoyuzString *delim) {
    void *list = soyuz_list_new(4, soyuz_list_dtor_rc);
    if (!s || !delim) return list;
    
    const char *data = soyuz_str_data(s);
    const char *ddata = soyuz_str_data(delim);
    int64_t dlen = delim->len;
    
    if (dlen == 0) return list; 
    
    const char *p = data;
    const char *next;
    while ((next = strstr(p, ddata)) != NULL) {
        int64_t chunk = next - p;
        SoyuzString *sub = soyuz_str_new(p, chunk);
        soyuz_list_append(list, sub);
        p = next + dlen;
    }
    SoyuzString *sub = soyuz_str_new(p, strlen(p));
    soyuz_list_append(list, sub);
    return list;
}

// --- FFI Numerical Parsers ---

int64_t soyuz_str_to_int(SoyuzString *s) {
    parse_err = 0;
    if (!s || s->len == 0) { parse_err = 1; return 0; }
    char *endptr;
    long long val = strtoll(soyuz_str_data(s), &endptr, 10);
    if (*endptr != '\0') { parse_err = 1; return 0; }
    return (int64_t)val;
}

double soyuz_str_to_float(SoyuzString *s) {
    parse_err = 0;
    if (!s || s->len == 0) { parse_err = 1; return 0.0; }
    char *endptr;
    double val = strtod(soyuz_str_data(s), &endptr);
    if (*endptr != '\0') { parse_err = 1; return 0.0; }
    return val;
}

int64_t soyuz_int_abs(int64_t n) {
    return n < 0 ? -n : n;
}

double soyuz_int_to_float(int64_t n) {
    return (double)n;
}

SoyuzString *soyuz_int_to_str(int64_t n) {
    char buf[32];
    snprintf(buf, sizeof(buf), "%lld", (long long)n);
    return soyuz_str_from_cstr(buf);
}

// Soyuz Standard Library — I/O mock (FFI via extern fn)
// Linked by the Soyuz compiler alongside rc.c.
// GC-safe: no Soyuz heap pointers are stored.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>

void soyuz_print_int(int64_t n) {
    printf("%ld\n", (long)n);
}

void soyuz_print_float(double x) {
    printf("%g\n", x);
}

void soyuz_print_bool(int64_t b) {
    printf("%s\n", b ? "true" : "false");
}

void soyuz_print_str(const char *s) {
    if (s) printf("%s\n", s);
}

// Print without trailing newline.
void soyuz_print(const char *s) {
    if (s) printf("%s", s);
}

int64_t soyuz_str_len(const char *s) {
    return s ? (int64_t)strlen(s) : 0;
}

// Returns a heap-allocated string — caller must free (or let RC handle it via extern).
const char *soyuz_int_to_str(int64_t n) {
    char *buf = (char *)malloc(32);
    if (buf) snprintf(buf, 32, "%ld", (long)n);
    return buf;
}

const char *soyuz_float_to_str(double x) {
    char *buf = (char *)malloc(32);
    if (buf) snprintf(buf, 32, "%g", x);
    return buf;
}

// Reads a line from stdin (no trailing newline). Caller must free.
const char *soyuz_read_line(void) {
    char *buf = NULL;
    size_t cap = 0;
    ssize_t len = getline(&buf, &cap, stdin);
    if (len < 0) {
        free(buf);
        return NULL;
    }
    // strip trailing newline
    if (len > 0 && buf[len - 1] == '\n') buf[len - 1] = '\0';
    return buf;
}

void soyuz_exit(int64_t code) {
    exit((int)code);
}

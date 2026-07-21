// Soyuz Standard Library — I/O mock (FFI via extern fn)
// Linked by the Soyuz compiler alongside rc.c.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include "soyuz.h"

void soyuz_print_int(int64_t n) {
    printf("%ld\n", (long)n);
}

void soyuz_print_float(double x) {
    printf("%g\n", x);
}

void soyuz_print_bool(int64_t b) {
    printf("%s\n", b ? "true" : "false");
}

void soyuz_print_str(SoyuzString *s) {
    if (s) printf("%s\n", soyuz_str_data(s));
}

// Print without trailing newline.
void soyuz_print(SoyuzString *s) {
    if (s) printf("%s", soyuz_str_data(s));
}

SoyuzString *soyuz_float_to_str(double x) {
    char *buf = (char *)malloc(32);
    if (!buf) return soyuz_str_new("", 0);
    snprintf(buf, 32, "%g", x);
    SoyuzString *s = soyuz_str_from_cstr(buf);
    free(buf);
    return s;
}

// Reads a line from stdin (no trailing newline). Returns a SoyuzString*.
SoyuzString *soyuz_read_line(void) {
    char *buf = NULL;
    size_t cap = 0;
    ssize_t len = getline(&buf, &cap, stdin);
    if (len < 0) {
        free(buf);
        return soyuz_str_new("", 0);
    }
    // strip trailing newline
    if (len > 0 && buf[len - 1] == '\n') { buf[len - 1] = '\0'; len--; }
    SoyuzString *s = soyuz_str_new(buf, (int64_t)len);
    free(buf);
    return s;
}

void soyuz_exit(int64_t code) {
    exit((int)code);
}

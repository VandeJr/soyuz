// Soyuz Standard Library — OS operations (FFI via extern fn)
// Linked by the Soyuz compiler alongside rc.c, std_io.c, std_string.c, and std_fs.c.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <unistd.h>
#include "soyuz.h"

// environ is POSIX — lets us look up env vars without calling getenv() by name,
// which would conflict with any Soyuz user function also named "getenv".
extern char **environ;

static char os_err_buf[512] = "";

static void os_set_error(const char *msg) {
    snprintf(os_err_buf, sizeof(os_err_buf), "%s", msg);
}

// Direct environ scan — avoids calling getenv() whose symbol the linker may
// resolve to a user-defined Soyuz function named "getenv".
static const char *os_getenv_raw(const char *name) {
    if (!name || !environ) return NULL;
    size_t nlen = strlen(name);
    for (char **e = environ; *e; e++) {
        if (strncmp(*e, name, nlen) == 0 && (*e)[nlen] == '=')
            return (*e) + nlen + 1;
    }
    return NULL;
}

// Returns the value of environment variable `name`.
// Returns NULL and sets the error buffer if `name` is not set.
SoyuzString *soyuz_os_getenv(SoyuzString *name) {
    if (!name) {
        printf("!name\n");  
        os_set_error("soyuz_os_getenv: null name");
        return NULL;
    }
    const char *val = os_getenv_raw(soyuz_str_data(name));
    if (!val) {
        printf("!val\n");  
        char msg[600];
        snprintf(msg, sizeof(msg), "variável de ambiente não definida: %s", soyuz_str_data(name));
        os_set_error(msg);
        return NULL;
    }
os_err_buf[0] = '\0';
    return soyuz_str_from_cstr(val);
}

// Returns 1 if environment variable `name` is set, 0 otherwise.
int64_t soyuz_os_has_env(SoyuzString *name) {
    if (!name) return 0;
    return os_getenv_raw(soyuz_str_data(name)) != NULL ? 1 : 0;
}

// Returns the command-line arguments as a RC-managed SoyuzList* of SoyuzString*.
// Reads from /proc/self/cmdline (Linux). Returns an empty list on failure.
void *soyuz_os_args(void) {
    FILE *f = fopen("/proc/self/cmdline", "rb");
    void *list = soyuz_list_new(4, soyuz_list_dtor_rc);
    if (!f) return list;
    char buf[65536];
    size_t n = fread(buf, 1, sizeof(buf) - 1, f);
    fclose(f);
    buf[n] = '\0';
    size_t i = 0;
    while (i < n) {
        const char *arg = buf + i;
        size_t len = strlen(arg);
        SoyuzString *s = soyuz_str_new(arg, (int64_t)len);
        soyuz_list_append(list, (void *)s);
        i += len + 1;
    }
    return list;
}

// Executes `cmd` via popen and returns captured stdout as a SoyuzString*.
// Returns NULL on error; check soyuz_os_has_error() for the reason.
SoyuzString *soyuz_os_exec(SoyuzString *cmd) {
    if (!cmd) {
        os_set_error("soyuz_os_exec: null command");
        return NULL;
    }
    const char *ccmd = soyuz_str_data(cmd);
    FILE *p = popen(ccmd, "r");
    if (!p) {
        char msg[600];
        snprintf(msg, sizeof(msg), "popen failed for: %s", ccmd);
        os_set_error(msg);
        return NULL;
    }
    char *out = NULL;
    size_t cap = 0, len = 0;
    char tmp[4096];
    size_t nr;
    while ((nr = fread(tmp, 1, sizeof(tmp), p)) > 0) {
        if (len + nr + 1 > cap) {
            cap = (len + nr + 1) * 2 + 4096;
            char *grown = (char *)realloc(out, cap);
            if (!grown) { free(out); pclose(p); os_set_error("soyuz_os_exec: out of memory"); return NULL; }
            out = grown;
        }
        memcpy(out + len, tmp, nr);
        len += nr;
    }
    int status = pclose(p);
    if (out) out[len] = '\0';
    if (status != 0) {
        free(out);
        char msg[600];
        snprintf(msg, sizeof(msg), "command failed (exit %d): %s", status, ccmd);
        os_set_error(msg);
        return NULL;
    }
    os_err_buf[0] = '\0';
    SoyuzString *result = soyuz_str_new(out ? out : "", (int64_t)len);
    free(out);
    return result;
}

// Returns 1 if the last OS operation failed, 0 otherwise.
int64_t soyuz_os_has_error(void) {
    return os_err_buf[0] != '\0' ? 1 : 0;
}

// Returns the error message from the last failed OS operation.
SoyuzString *soyuz_os_last_error(void) {
    return soyuz_str_from_cstr(os_err_buf);
}

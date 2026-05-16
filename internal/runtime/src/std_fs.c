// Soyuz Standard Library — File System operations (FFI via extern fn)
// Linked by the Soyuz compiler alongside rc.c, std_io.c, and std_string.c.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <sys/stat.h>
#include <dirent.h>
#include <unistd.h>
#include <errno.h>
#include "soyuz.h"

// Thread-local error state for the last FS operation.
static char fs_err_buf[512] = "";

static void fs_set_error(const char *fmt, const char *arg) {
    if (arg) {
        snprintf(fs_err_buf, sizeof(fs_err_buf), fmt, arg);
    } else {
        snprintf(fs_err_buf, sizeof(fs_err_buf), "%s", fmt);
    }
}

static void fs_set_errno(const char *prefix, const char *path) {
    char tmp[512];
    snprintf(tmp, sizeof(tmp), "%s: %s", prefix, strerror(errno));
    if (path) {
        snprintf(fs_err_buf, sizeof(fs_err_buf), "%s (%s)", tmp, path);
    } else {
        snprintf(fs_err_buf, sizeof(fs_err_buf), "%s", tmp);
    }
}

// Reads an entire file into a new RC-managed SoyuzString.
SoyuzString *soyuz_fs_read_file(SoyuzString *path) {
    if (!path) {
        fs_set_error("soyuz_fs_read_file: path is null", NULL);
        return NULL;
    }
    const char *cpath = soyuz_str_data(path);
    FILE *f = fopen(cpath, "rb");
    if (!f) {
        fs_set_error("cannot open file for reading: %s", cpath);
        return NULL;
    }
    if (fseek(f, 0, SEEK_END) != 0) {
        fclose(f);
        fs_set_error("fseek failed: %s", cpath);
        return NULL;
    }
    long len = ftell(f);
    if (len < 0) {
        fclose(f);
        fs_set_error("ftell failed: %s", cpath);
        return NULL;
    }
    fseek(f, 0, SEEK_SET);
    char *buf = (char *)malloc((size_t)(len + 1));
    if (!buf) {
        fclose(f);
        fs_set_error("out of memory reading: %s", cpath);
        return NULL;
    }
    size_t nread = fread(buf, 1, (size_t)len, f);
    fclose(f);
    buf[nread] = '\0';
    fs_err_buf[0] = '\0';
    SoyuzString *result = soyuz_str_new(buf, (int64_t)nread);
    free(buf);
    return result;
}

// Writes content to path, creating or truncating the file.
int64_t soyuz_fs_write_file(SoyuzString *path, SoyuzString *content) {
    if (!path || !content) {
        fs_set_error("soyuz_fs_write_file: null argument", NULL);
        return 0;
    }
    const char *cpath = soyuz_str_data(path);
    FILE *f = fopen(cpath, "wb");
    if (!f) {
        fs_set_error("cannot open file for writing: %s", cpath);
        return 0;
    }
    size_t nwritten = fwrite(soyuz_str_data(content), 1, (size_t)content->len, f);
    fclose(f);
    if (nwritten != (size_t)content->len) {
        fs_set_error("write error: %s", cpath);
        return 0;
    }
    fs_err_buf[0] = '\0';
    return 1;
}

// Returns 1 if path exists (file or directory), 0 otherwise.
int64_t soyuz_fs_exists(SoyuzString *path) {
    if (!path) return 0;
    struct stat st;
    return stat(soyuz_str_data(path), &st) == 0 ? 1 : 0;
}

// Returns 1 if path is a directory, 0 otherwise.
int64_t soyuz_fs_is_dir(SoyuzString *path) {
    if (!path) return 0;
    struct stat st;
    if (stat(soyuz_str_data(path), &st) != 0) return 0;
    return S_ISDIR(st.st_mode) ? 1 : 0;
}

// Returns 1 if the last FS operation failed, 0 otherwise.
int64_t soyuz_fs_has_error(void) {
    return fs_err_buf[0] != '\0' ? 1 : 0;
}

// Returns the error message from the last failed FS operation.
SoyuzString *soyuz_fs_last_error(void) {
    return soyuz_str_from_cstr(fs_err_buf);
}

// Creates a directory (non-recursive). Returns 1 on success, 0 on failure.
int64_t soyuz_fs_mkdir(SoyuzString *path) {
    if (!path) { fs_set_error("soyuz_fs_mkdir: path is null", NULL); return 0; }
    const char *cpath = soyuz_str_data(path);
    if (mkdir(cpath, 0755) != 0) {
        fs_set_errno("mkdir failed", cpath);
        return 0;
    }
    fs_err_buf[0] = '\0';
    return 1;
}

// Removes an empty directory. Returns 1 on success, 0 on failure.
int64_t soyuz_fs_rmdir(SoyuzString *path) {
    if (!path) { fs_set_error("soyuz_fs_rmdir: path is null", NULL); return 0; }
    const char *cpath = soyuz_str_data(path);
    if (rmdir(cpath) != 0) {
        fs_set_errno("rmdir failed", cpath);
        return 0;
    }
    fs_err_buf[0] = '\0';
    return 1;
}

// Renames/moves a file within the same filesystem. Returns 1 on success, 0 on failure.
int64_t soyuz_fs_rename(SoyuzString *from, SoyuzString *to) {
    if (!from || !to) { fs_set_error("soyuz_fs_rename: null argument", NULL); return 0; }
    if (rename(soyuz_str_data(from), soyuz_str_data(to)) != 0) {
        fs_set_errno("rename failed", soyuz_str_data(from));
        return 0;
    }
    fs_err_buf[0] = '\0';
    return 1;
}

// Moves a file (rename first; falls back to copy+delete across filesystems).
int64_t soyuz_fs_move(SoyuzString *from, SoyuzString *to) {
    if (!from || !to) { fs_set_error("soyuz_fs_move: null argument", NULL); return 0; }
    const char *cfrom = soyuz_str_data(from);
    const char *cto   = soyuz_str_data(to);
    // Fast path: rename (same filesystem).
    if (rename(cfrom, cto) == 0) { fs_err_buf[0] = '\0'; return 1; }
    // Cross-device: copy then unlink.
    FILE *src = fopen(cfrom, "rb");
    if (!src) { fs_set_errno("move/open-src failed", cfrom); return 0; }
    FILE *dst = fopen(cto, "wb");
    if (!dst) { fclose(src); fs_set_errno("move/open-dst failed", cto); return 0; }
    char buf[65536];
    size_t n;
    int ok = 1;
    while ((n = fread(buf, 1, sizeof(buf), src)) > 0) {
        if (fwrite(buf, 1, n, dst) != n) { ok = 0; break; }
    }
    fclose(src);
    fclose(dst);
    if (!ok) { fs_set_errno("move/write failed", cto); return 0; }
    if (unlink(cfrom) != 0) { fs_set_errno("move/unlink failed", cfrom); return 0; }
    fs_err_buf[0] = '\0';
    return 1;
}

// Copies a file. Returns 1 on success, 0 on failure.
int64_t soyuz_fs_copy(SoyuzString *from, SoyuzString *to) {
    if (!from || !to) { fs_set_error("soyuz_fs_copy: null argument", NULL); return 0; }
    const char *cfrom = soyuz_str_data(from);
    const char *cto   = soyuz_str_data(to);
    FILE *src = fopen(cfrom, "rb");
    if (!src) { fs_set_errno("copy/open-src failed", cfrom); return 0; }
    FILE *dst = fopen(cto, "wb");
    if (!dst) { fclose(src); fs_set_errno("copy/open-dst failed", cto); return 0; }
    char buf[65536];
    size_t n;
    int ok = 1;
    while ((n = fread(buf, 1, sizeof(buf), src)) > 0) {
        if (fwrite(buf, 1, n, dst) != n) { ok = 0; break; }
    }
    fclose(src);
    fclose(dst);
    if (!ok) { fs_set_errno("copy/write failed", cto); return 0; }
    fs_err_buf[0] = '\0';
    return 1;
}

// Creates a unique temporary directory using the given prefix template.
// The template must end with at least six X's (passed to mkdtemp(3)).
// Returns the created path, or NULL on failure.
SoyuzString *soyuz_fs_mkdtemp(SoyuzString *prefix) {
    if (!prefix) { fs_set_error("soyuz_fs_mkdtemp: prefix is null", NULL); return NULL; }
    const char *cpfx = soyuz_str_data(prefix);
    // Build a mutable template: prefix + "XXXXXX"
    size_t plen = strlen(cpfx);
    char *tmpl = (char *)malloc(plen + 7);
    if (!tmpl) { fs_set_error("soyuz_fs_mkdtemp: out of memory", NULL); return NULL; }
    memcpy(tmpl, cpfx, plen);
    memcpy(tmpl + plen, "XXXXXX", 7);
    if (mkdtemp(tmpl) == NULL) {
        fs_set_errno("mkdtemp failed", cpfx);
        free(tmpl);
        return NULL;
    }
    fs_err_buf[0] = '\0';
    SoyuzString *result = soyuz_str_from_cstr(tmpl);
    free(tmpl);
    return result;
}

// Returns a List[String] of entry names in a directory (excluding . and ..).
void *soyuz_fs_read_dir(SoyuzString *path) {
    void *list = soyuz_list_new(16, soyuz_list_dtor_rc);
    if (!path) { fs_set_error("soyuz_fs_read_dir: path is null", NULL); return list; }
    const char *cpath = soyuz_str_data(path);
    DIR *d = opendir(cpath);
    if (!d) { fs_set_errno("readDir failed", cpath); return list; }
    struct dirent *entry;
    while ((entry = readdir(d)) != NULL) {
        if (strcmp(entry->d_name, ".") == 0 || strcmp(entry->d_name, "..") == 0) continue;
        SoyuzString *name = soyuz_str_from_cstr(entry->d_name);
        soyuz_list_append(list, name);
    }
    closedir(d);
    fs_err_buf[0] = '\0';
    return list;
}

// Opens a file. Returns FILE* cast to int64 (non-zero = success, 0 = error).
int64_t soyuz_fs_open(SoyuzString *path, SoyuzString *mode) {
    if (!path || !mode) { fs_set_error("soyuz_fs_open: null argument", NULL); return 0; }
    const char *cpath = soyuz_str_data(path);
    FILE *f = fopen(cpath, soyuz_str_data(mode));
    if (!f) { fs_set_errno("open failed", cpath); return 0; }
    fs_err_buf[0] = '\0';
    return (int64_t)(uintptr_t)f;
}

// Closes a file handle.
void soyuz_fs_close(int64_t fd) {
    if (fd == 0) return;
    fclose((FILE *)(uintptr_t)fd);
}

// Reads remaining content from an open file handle into a new SoyuzString.
SoyuzString *soyuz_fs_file_read(int64_t fd) {
    if (fd == 0) { fs_set_error("file_read: invalid file handle", NULL); return NULL; }
    FILE *f = (FILE *)(uintptr_t)fd;
    long start = ftell(f);
    if (fseek(f, 0, SEEK_END) != 0) { fs_set_errno("file_read/fseek", NULL); return NULL; }
    long end = ftell(f);
    fseek(f, start, SEEK_SET);
    long len = end - start;
    if (len < 0) len = 0;
    char *buf = (char *)malloc((size_t)(len + 1));
    if (!buf) { fs_set_error("file_read: out of memory", NULL); return NULL; }
    size_t nread = fread(buf, 1, (size_t)len, f);
    buf[nread] = '\0';
    fs_err_buf[0] = '\0';
    SoyuzString *result = soyuz_str_new(buf, (int64_t)nread);
    free(buf);
    return result;
}

// Writes content to an open file handle. Returns 1 on success, 0 on failure.
int64_t soyuz_fs_file_write(int64_t fd, SoyuzString *content) {
    if (fd == 0 || !content) { fs_set_error("file_write: invalid argument", NULL); return 0; }
    FILE *f = (FILE *)(uintptr_t)fd;
    size_t nwritten = fwrite(soyuz_str_data(content), 1, (size_t)content->len, f);
    if (nwritten != (size_t)content->len) { fs_set_errno("file_write failed", NULL); return 0; }
    fs_err_buf[0] = '\0';
    return 1;
}

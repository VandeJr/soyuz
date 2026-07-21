// Soyuz Test Runtime — test runner support
// Provides: soyuz_run_one_test, soyuz_print_test_results, soyuz_test_assert_*
#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include <stdbool.h>
#include <setjmp.h>
#include <time.h>
#include "soyuz.h"

#define ANSI_GREEN "\033[0;32m"
#define ANSI_RED   "\033[0;31m"
#define ANSI_BOLD  "\033[1m"
#define ANSI_DIM   "\033[2m"
#define ANSI_RESET "\033[0m"

#define MAX_FAIL_MSG 512

static jmp_buf  _srt_jmp_buf;
static char     _srt_fail_msg[MAX_FAIL_MSG];
static bool     _srt_failed;

static int _srt_passed = 0;
static int _srt_failed_count = 0;
static int _srt_total = 0;

// ─── assert primitives ────────────────────────────────────────────────────────

void soyuz_test_assert_bool(int64_t cond, SoyuzString *msg) {
    if (!cond) {
        _srt_failed = true;
        const char *raw = msg ? soyuz_str_data(msg) : "assert falhou";
        strncpy(_srt_fail_msg, raw, MAX_FAIL_MSG - 1);
        _srt_fail_msg[MAX_FAIL_MSG - 1] = '\0';
        longjmp(_srt_jmp_buf, 1);
    }
}

void soyuz_test_assert_eq_int(int64_t a, int64_t b) {
    if (a != b) {
        char buf[MAX_FAIL_MSG];
        snprintf(buf, MAX_FAIL_MSG, "esperava %lld, mas obteve %lld", (long long)b, (long long)a);
        _srt_failed = true;
        strncpy(_srt_fail_msg, buf, MAX_FAIL_MSG - 1);
        _srt_fail_msg[MAX_FAIL_MSG - 1] = '\0';
        longjmp(_srt_jmp_buf, 1);
    }
}

void soyuz_test_assert_eq_str(SoyuzString *a, SoyuzString *b) {
    const char *sa = a ? soyuz_str_data(a) : "";
    const char *sb = b ? soyuz_str_data(b) : "";
    if (strcmp(sa, sb) != 0) {
        char buf[MAX_FAIL_MSG];
        snprintf(buf, MAX_FAIL_MSG, "esperava \"%s\", mas obteve \"%s\"", sb, sa);
        _srt_failed = true;
        strncpy(_srt_fail_msg, buf, MAX_FAIL_MSG - 1);
        _srt_fail_msg[MAX_FAIL_MSG - 1] = '\0';
        longjmp(_srt_jmp_buf, 1);
    }
}

void soyuz_test_assert_eq_float(double a, double b) {
    if (a != b) {
        char buf[MAX_FAIL_MSG];
        snprintf(buf, MAX_FAIL_MSG, "esperava %g, mas obteve %g", b, a);
        _srt_failed = true;
        strncpy(_srt_fail_msg, buf, MAX_FAIL_MSG - 1);
        _srt_fail_msg[MAX_FAIL_MSG - 1] = '\0';
        longjmp(_srt_jmp_buf, 1);
    }
}

// ─── runner ───────────────────────────────────────────────────────────────────

void soyuz_run_one_test(const char *name, void (*fn)(void)) {
    _srt_failed = false;
    _srt_fail_msg[0] = '\0';
    _srt_total++;

    struct timespec t0, t1;
    clock_gettime(CLOCK_MONOTONIC, &t0);

    if (setjmp(_srt_jmp_buf) == 0) {
        fn();
        clock_gettime(CLOCK_MONOTONIC, &t1);
        double ms = (t1.tv_sec - t0.tv_sec) * 1000.0 +
                    (t1.tv_nsec - t0.tv_nsec) / 1e6;
        printf(ANSI_GREEN "✓" ANSI_RESET " %s " ANSI_DIM "(%.1fms)" ANSI_RESET "\n",
               name, ms);
        _srt_passed++;
    } else {
        clock_gettime(CLOCK_MONOTONIC, &t1);
        double ms = (t1.tv_sec - t0.tv_sec) * 1000.0 +
                    (t1.tv_nsec - t0.tv_nsec) / 1e6;
        printf(ANSI_RED "✗" ANSI_RESET " %s " ANSI_DIM "(%.1fms)" ANSI_RESET
               ": %s\n",
               name, ms, _srt_fail_msg);
        _srt_failed_count++;
    }
}

int32_t soyuz_print_test_results(void) {
    printf("\n");
    if (_srt_total == 0) {
        printf(ANSI_DIM "nenhum teste encontrado" ANSI_RESET "\n");
        return 0;
    }
    if (_srt_failed_count == 0) {
        printf(ANSI_GREEN ANSI_BOLD "✓ %d %s passaram" ANSI_RESET "\n",
               _srt_passed, _srt_passed == 1 ? "teste" : "testes");
        return 0;
    }
    printf(ANSI_RED ANSI_BOLD "%d falhou" ANSI_RESET
           ", %d passou (%d total)\n",
           _srt_failed_count, _srt_passed, _srt_total);
    return 1;
}

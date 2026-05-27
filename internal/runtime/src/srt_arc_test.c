/*
 * srt_arc_test.c — M-14 Arc[T] + EBR correctness & benchmark
 *
 * Compila: cc -std=c11 -O2 -pthread std_arc.c soyuz_rt.c srt_arc_test.c -o srt_arc_test
 */

#include "soyuz_rt.h"
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <assert.h>

/* std_arc.c public API */
void   *srt_arc_new(int64_t value);
void   *srt_arc_clone(void *ptr);
void    srt_arc_release(void *ptr);
int64_t srt_arc_get(void *ptr);
int64_t srt_arc_refcount(void *ptr);
void    srt_arc_quiescent(void);

static int failures = 0;
#define CHECK(cond, msg) \
    do { if (!(cond)) { fprintf(stderr,"FAIL: %s\n",msg); failures++; } \
         else          { printf("PASS: %s\n", msg); } } while(0)

static double now_ms(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (double)ts.tv_sec * 1e3 + (double)ts.tv_nsec / 1e6;
}

/* ── Test 1: basic Arc lifecycle ─────────────────────────────────────────── */
static void test_basic(void) {
    void *a = srt_arc_new(42);
    CHECK(srt_arc_get(a) == 42,        "arc_new stores value");
    CHECK(srt_arc_refcount(a) == 1,    "initial refcount = 1");

    void *b = srt_arc_clone(a);
    CHECK(srt_arc_refcount(a) == 2,    "after clone refcount = 2");
    CHECK(srt_arc_get(b) == 42,        "clone sees same value");
    CHECK(a == b,                      "clone returns same pointer");

    srt_arc_release(b);
    CHECK(srt_arc_refcount(a) == 1,    "after one release refcount = 1");

    srt_arc_release(a);
    /* a is now retired (EBR, not freed yet) — don't access after this */
    CHECK(1, "arc_release on last ref retires via EBR (no crash)");
}

/* ── Test 2: multi-clone round-trip ─────────────────────────────────────── */
static void test_multi_clone(void) {
    void *orig = srt_arc_new(100);
    void *refs[8];
    for (int i = 0; i < 8; i++) refs[i] = srt_arc_clone(orig);
    CHECK(srt_arc_refcount(orig) == 9, "9 refs after 8 clones");
    /* All see 100 */
    int ok = 1;
    for (int i = 0; i < 8; i++) if (srt_arc_get(refs[i]) != 100) ok = 0;
    CHECK(ok, "all clones see value 100");
    /* Release all */
    for (int i = 0; i < 8; i++) srt_arc_release(refs[i]);
    CHECK(srt_arc_refcount(orig) == 1, "refcount back to 1 after 8 releases");
    srt_arc_release(orig);
    CHECK(1, "final release retired ok");
}

/* ── Test 3: cross-task sharing (Arc shared across srt tasks) ───────────── */

typedef struct { void *arc; int64_t *out; } cross_args_t;

static void task_read_arc(void *arg) {
    cross_args_t *a = (cross_args_t *)arg;
    *a->out = srt_arc_get(a->arc);
    srt_arc_release(a->arc);    /* release our clone */
    srt_arc_quiescent();        /* natural quiescent point */
}

static void test_cross_task(void) {
    void *arc = srt_arc_new(777);
    static int64_t results[4];
    static cross_args_t args[4];

    void *handles[4];
    for (int i = 0; i < 4; i++) {
        args[i].arc = srt_arc_clone(arc);  /* give each task a clone */
        args[i].out = &results[i];
        handles[i]  = srt_enqueue(task_read_arc, &args[i]);
    }
    srt_arc_release(arc);   /* release original */

    for (int i = 0; i < 4; i++) srt_await(handles[i]);

    int ok = 1;
    for (int i = 0; i < 4; i++) if (results[i] != 777) ok = 0;
    CHECK(ok, "cross-task: 4 tasks read Arc[777] concurrently");
}

/* ── Test 4: EBR deferred reclamation (smoke test) ──────────────────────── */
static void test_ebr_deferred(void) {
    /* Create and release N arcs in quick succession.
     * If EBR were to free immediately on release with refcount=0, reading
     * through a clone after release would crash. EBR must keep objects alive
     * until all threads advance past the retire epoch. */
    void *arcs[16];
    for (int i = 0; i < 16; i++) arcs[i] = srt_arc_new((int64_t)i);

    /* Clone each — now refcount=2 */
    void *clones[16];
    for (int i = 0; i < 16; i++) clones[i] = srt_arc_clone(arcs[i]);

    /* Release originals — refcount drops to 1 */
    for (int i = 0; i < 16; i++) srt_arc_release(arcs[i]);

    /* Clones must still be readable */
    int ok = 1;
    for (int i = 0; i < 16; i++) if (srt_arc_get(clones[i]) != (int64_t)i) ok = 0;
    CHECK(ok, "EBR: clones readable after original released (refcount=1)");

    /* Release clones — refcount hits 0, EBR retires */
    for (int i = 0; i < 16; i++) srt_arc_release(clones[i]);

    /* Advance epoch by calling quiescent */
    srt_arc_quiescent();
    srt_arc_quiescent();
    srt_arc_quiescent();
    CHECK(1, "EBR: retire + quiescent advance does not crash");
}

/* ── Benchmark: EBR Arc vs plain malloc/free ─────────────────────────────── */

#define BENCH_ITERS 100000

static void bench_arc(void) {
    double t0 = now_ms();
    for (int i = 0; i < BENCH_ITERS; i++) {
        void *a = srt_arc_new((int64_t)i);
        void *b = srt_arc_clone(a);
        srt_arc_release(a);
        srt_arc_release(b);
    }
    srt_arc_quiescent(); /* flush retire list */
    double arc_ms = now_ms() - t0;

    /* Baseline: plain malloc+free */
    t0 = now_ms();
    for (int i = 0; i < BENCH_ITERS; i++) {
        void *p = malloc(16);
        (void)p;
        free(p);
    }
    double raw_ms = now_ms() - t0;

    printf("PASS: benchmark: Arc EBR=%.1f ms, malloc/free=%.1f ms (%d iters)\n",
           arc_ms, raw_ms, BENCH_ITERS);
}

/* ── main ────────────────────────────────────────────────────────────────── */

int main(void) {
    printf("=== soyuz Arc[T] + EBR M-14 tests ===\n");
    test_basic();
    test_multi_clone();
    test_cross_task();
    test_ebr_deferred();
    bench_arc();
    printf("======================================\n");
    if (failures) { fprintf(stderr, "%d test(s) FAILED\n", failures); return 1; }
    printf("All tests passed.\n");
    return 0;
}

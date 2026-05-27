/*
 * srt_bench.c — M-13 work-stealing benchmark + testes de correctness
 *
 * Compila standalone: cc -std=c11 -O2 -pthread soyuz_rt.c srt_bench.c -o srt_bench
 *
 * Testes:
 *   1. work-stealing: gera MANY tasks de um único thread — outros threads devem roubar
 *   2. throughput: N tasks que retornam imediatamente — conta tasks/sec
 *   3. recursive fan-out: task spawna filhos (testa stealing de tasks geradas por tasks)
 */

#include "soyuz_rt.h"
#include <stdio.h>
#include <stdlib.h>
#include <stdint.h>
#include <string.h>
#include <time.h>
#include <assert.h>

static int failures = 0;
#define CHECK(cond, msg) \
    do { \
        if (!(cond)) { fprintf(stderr, "FAIL: %s\n", msg); failures++; } \
        else           { printf("PASS: %s\n", msg); } \
    } while(0)

/* ── Helpers ─────────────────────────────────────────────────────────────── */

static double now_ms(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (double)ts.tv_sec * 1e3 + (double)ts.tv_nsec / 1e6;
}

/* ── Test 1: work-stealing correctness ──────────────────────────────────── */
/*
 * Spawn 256 tasks, each returns its index squared.
 * We enqueue ALL of them before awaiting, so worker threads must steal to
 * make progress (one thread alone couldn't process all in time).
 */
#define WS_N 256
static int64_t ws_input[WS_N];
static int64_t ws_out[WS_N];

static void ws_task(void *arg) {
    int64_t *idx = (int64_t *)arg;
    int i = (int)*idx;
    ws_out[i] = (int64_t)(i) * (int64_t)(i);
    srt_set_task_result((void *)&ws_out[i]);
}

static void test_work_stealing(void) {
    void *handles[WS_N];
    for (int i = 0; i < WS_N; i++) {
        ws_input[i] = i;
        handles[i] = srt_enqueue(ws_task, &ws_input[i]);
    }
    int ok = 1;
    for (int i = 0; i < WS_N; i++) {
        void *r = srt_await(handles[i]);
        int64_t *val = (int64_t *)r;
        if (!val || *val != (int64_t)i * i) ok = 0;
    }
    CHECK(ok, "work-stealing: 256 tasks, i*i correctness");
}

/* ── Test 2: recursive fan-out (tasks spawning tasks) ───────────────────── */

static int64_t fanout_results[8];
static int64_t fanout_indices[8] = {0,1,2,3,4,5,6,7};

static void fanout_leaf(void *arg) {
    int64_t *idx = (int64_t *)arg;
    int i = (int)*idx;
    fanout_results[i] = i * 10;
    srt_set_task_result((void *)&fanout_results[i]);
}

/* Outer task spawns 8 inner tasks and awaits them all */
static int64_t fanout_sum;
static void fanout_root(void *arg) {
    (void)arg;
    void *handles[8];
    for (int i = 0; i < 8; i++)
        handles[i] = srt_enqueue(fanout_leaf, &fanout_indices[i]);
    int64_t sum = 0;
    for (int i = 0; i < 8; i++) {
        void *r = srt_await(handles[i]);
        sum += *(int64_t *)r;
    }
    fanout_sum = sum;
    srt_set_task_result((void *)&fanout_sum);
}

static void test_recursive_fanout(void) {
    void *handle = srt_enqueue(fanout_root, NULL);
    void *result = srt_await(handle);
    int64_t *sum = (int64_t *)result;
    /* sum = 0+10+20+30+40+50+60+70 = 280 */
    CHECK(sum && *sum == 280, "recursive fan-out: root awaits 8 children → sum=280");
}

/* ── Test 3: throughput benchmark ───────────────────────────────────────── */

#define BENCH_N 4096
static int64_t bench_vals[BENCH_N];

static void bench_noop(void *arg) {
    int64_t *n = (int64_t *)arg;
    *n = (*n) + 1;
    srt_set_task_result((void *)n);
}

static void test_throughput(void) {
    for (int i = 0; i < BENCH_N; i++) bench_vals[i] = i;
    double t0 = now_ms();
    void *handles[BENCH_N];
    for (int i = 0; i < BENCH_N; i++)
        handles[i] = srt_enqueue(bench_noop, &bench_vals[i]);
    for (int i = 0; i < BENCH_N; i++)
        srt_await(handles[i]);
    double elapsed = now_ms() - t0;
    double tps = (double)BENCH_N / (elapsed / 1000.0);
    printf("PASS: throughput: %d tasks in %.1f ms → %.0f tasks/sec\n",
           BENCH_N, elapsed, tps);
}

/* ── main ────────────────────────────────────────────────────────────────── */

int main(void) {
    printf("=== soyuz_rt M-13 work-stealing tests ===\n");
    test_work_stealing();
    test_recursive_fanout();
    test_throughput();
    printf("==========================================\n");
    if (failures) { fprintf(stderr, "%d test(s) FAILED\n", failures); return 1; }
    printf("All tests passed.\n");
    return 0;
}

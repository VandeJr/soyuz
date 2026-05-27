/*
 * srt_test.c — M-12 runtime integration tests
 *
 * Testa que o scheduler cooperativo funciona corretamente:
 * 1. Task básica: enqueue + await retorna resultado
 * 2. Task chain: t1 awaita t2 dentro de uma task (coroutine yield)
 * 3. Múltiplas tasks em paralelo
 * 4. Task.all — await de múltiplas tasks
 */

#include "soyuz_rt.h"
#include <stdio.h>
#include <stdlib.h>
#include <assert.h>
#include <string.h>

/* ── Helpers ─────────────────────────────────────────────────────────────── */

static int failures = 0;

#define CHECK(cond, msg) \
    do { \
        if (!(cond)) { \
            fprintf(stderr, "FAIL: %s\n", msg); \
            failures++; \
        } else { \
            printf("PASS: %s\n", msg); \
        } \
    } while(0)

/* ── Test 1: Basic enqueue + await ──────────────────────────────────────── */

static void task_return_42(void *args) {
    (void)args;
    static int64_t result = 42;
    srt_set_task_result((void *)&result);
}

static void test_basic_await(void) {
    void *handle = srt_enqueue(task_return_42, NULL);
    void *result = srt_await(handle);
    int64_t *r = (int64_t *)result;
    CHECK(r != NULL && *r == 42, "basic await returns 42");
}

/* ── Test 2: Task chain — task awaits another task (coroutine yield) ─────── */

static void inner_task(void *args) {
    (void)args;
    static int64_t val = 100;
    srt_set_task_result((void *)&val);
}

static void outer_task(void *args) {
    (void)args;
    /* This srt_await is called from inside a task — triggers coroutine yield */
    void *inner_handle = srt_enqueue(inner_task, NULL);
    void *inner_result = srt_await(inner_handle);
    /* inner_result should be 100 */
    static int64_t doubled;
    doubled = *((int64_t *)inner_result) * 2;  /* 200 */
    srt_set_task_result((void *)&doubled);
}

static void test_chain_await(void) {
    void *handle = srt_enqueue(outer_task, NULL);
    void *result = srt_await(handle);
    int64_t *r = (int64_t *)result;
    CHECK(r != NULL && *r == 200, "chain await: outer awaits inner → 200");
}

/* ── Test 3: Parallel tasks ─────────────────────────────────────────────── */

static int64_t g_counters[4] = {1, 2, 3, 4};

static void task_multiply(void *args) {
    int64_t *n = (int64_t *)args;
    static int64_t results[4];
    int idx = (int)(n - g_counters);
    results[idx] = (*n) * 10;
    srt_set_task_result((void *)&results[idx]);
}

static void test_parallel(void) {
    void *handles[4];
    for (int i = 0; i < 4; i++)
        handles[i] = srt_enqueue(task_multiply, &g_counters[i]);

    int64_t expected[4] = {10, 20, 30, 40};
    int ok = 1;
    for (int i = 0; i < 4; i++) {
        void *result = srt_await(handles[i]);
        int64_t *r = (int64_t *)result;
        if (!r || *r != expected[i]) ok = 0;
    }
    CHECK(ok, "parallel tasks: 4 tasks, each multiplies by 10");
}

/* ── Test 4: srt_detach (no leak) ───────────────────────────────────────── */

static void task_noop(void *args) {
    (void)args;
}

static void test_detach(void) {
    void *handle = srt_enqueue(task_noop, NULL);
    srt_detach(handle);
    /* No crash = pass */
    CHECK(1, "detach does not crash");
}

/* ── Test 5: Many tasks (stress) ────────────────────────────────────────── */

#define STRESS_N 64
static int64_t g_stress_results[STRESS_N];

static void task_stress(void *args) {
    int64_t *idx_ptr = (int64_t *)args;
    int idx = (int)*idx_ptr;
    g_stress_results[idx] = idx * idx;
    srt_set_task_result((void *)&g_stress_results[idx]);
}

static void test_stress(void) {
    static int64_t indices[STRESS_N];
    void *handles[STRESS_N];
    for (int i = 0; i < STRESS_N; i++) {
        indices[i] = i;
        handles[i] = srt_enqueue(task_stress, &indices[i]);
    }
    int ok = 1;
    for (int i = 0; i < STRESS_N; i++) {
        void *result = srt_await(handles[i]);
        int64_t *r = (int64_t *)result;
        if (!r || *r != (int64_t)(i * i)) ok = 0;
    }
    CHECK(ok, "stress: 64 tasks, each computes i*i");
}

/* ── Main ────────────────────────────────────────────────────────────────── */

int main(void) {
    printf("=== soyuz_rt M-12 tests ===\n");
    test_basic_await();
    test_chain_await();
    test_parallel();
    test_detach();
    test_stress();
    printf("===========================\n");
    if (failures > 0) {
        fprintf(stderr, "%d test(s) FAILED\n", failures);
        return 1;
    }
    printf("All tests passed.\n");
    return 0;
}

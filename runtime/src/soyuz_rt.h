#pragma once
#include <stdint.h>
#include <pthread.h>
#include <ucontext.h>

/* ── XSAVE overflow guard ────────────────────────────────────────────────────
 * On CPUs with AVX / AVX-512, swapcontext internally runs xsave to persist
 * FPU/SIMD state.  The CPU's XSAVE area can require up to 2440 bytes, but
 * ucontext_t.__fpregs_mem only holds 512 bytes (a legacy SSE-era assumption).
 * The overflow corrupts whatever sits after the ucontext_t in memory — on the
 * stack that means the stack canary and local variables, causing SIGSEGV or
 * "stack smashing detected".
 *
 * Fix: wrap every ucontext_t we own in srt_ctx_t, which appends SRT_CTX_XSAVE_PAD
 * bytes of dead space so the xsave overflow lands harmlessly in the padding.
 * (2440 - 512 = 1928 bytes needed; 2048 gives a comfortable safety margin.)
 * ─────────────────────────────────────────────────────────────────────────── */
#define SRT_CTX_XSAVE_PAD 2048

typedef struct {
    ucontext_t  uc;
    char        _xpad[SRT_CTX_XSAVE_PAD];
} srt_ctx_t;

/* ── Task state ───────────────────────────────────────────────────────────── */
typedef enum {
    SRT_PENDING   = 0,
    SRT_RUNNING   = 1,
    SRT_DONE      = 2,
    SRT_CANCELLED = 3,
    SRT_WAITING   = 4,  /* M-12: blocked in srt_await, yielded to scheduler */
} srt_task_state_t;

/* SRT_WAITING=4 > SRT_DONE=2, so ">= SRT_DONE" incorrectly matches WAITING.
 * Always use this predicate instead of raw ">= SRT_DONE" comparisons. */
static inline int srt_finished(int32_t s) {
    return s == SRT_DONE || s == SRT_CANCELLED;
}

/* ── M-10: Task tree ─────────────────────────────────────────────────────── */
typedef struct srt_task srt_task_t;

typedef struct srt_child_node {
    srt_task_t            *child;
    struct srt_child_node *next;
} srt_child_node_t;

/* ── Internal task handle — opaque to Soyuz generated code ─────────────── */
struct srt_task {
    void           (*fn)(void *);
    void            *args;
    void            *result;
    int32_t          state;     /* srt_task_state_t, updated with __atomic_* */
    int32_t          detached;
    int32_t          refcount;  /* starts at 2: caller + worker */
    double           progress;

    pthread_mutex_t  mu;
    pthread_cond_t   done_cond; /* for main-thread blocking await */

    /* M-10: task tree */
    srt_task_t       *parent;
    srt_child_node_t *children;
    pthread_mutex_t   children_mu;

    /* M-12: coroutine context */
    srt_ctx_t        coro_ctx;      /* saved context of this task (padded for XSAVE) */
    char            *stack;         /* private stack (NULL = not yet started) */
    srt_ctx_t       *sched_ctx;     /* scheduler ctx of the worker currently running this task */

    /* M-12: intrusive waiter list — tasks SRT_WAITING for THIS task */
    srt_task_t      *waiters;       /* head of list (protected by waiters_mu) */
    srt_task_t      *waiter_next;   /* next in the waiter list of some other task */
    pthread_mutex_t  waiters_mu;

    /* M-20: mutex to unlock in the scheduler AFTER swapcontext saves coro_ctx.
     * Prevents wake_waiters from dispatching this task before its coroutine
     * context is fully saved (race between waiters_mu unlock and swapcontext). */
    pthread_mutex_t *pending_unlock;
};

/* ── Public API (unchanged from M-01 through M-11) ─────────────────────── */

void        srt_init(int n_threads);
void       *srt_enqueue(void (*fn)(void *), void *args);
void       *srt_await(void *handle);
void        srt_set_task_result(void *result);
void        srt_detach(void *handle);
void        srt_cancel(void *handle);
void        srt_drop_task_handle(void *handle);
void       *srt_await_any(void **handles, int n);
void        srt_shutdown(void);
srt_task_t *srt_current_task(void);
void       *srt_task_handle_current(void);
int         srt_task_cancelled(void *handle);
void        srt_task_set_progress(void *handle, double progress);

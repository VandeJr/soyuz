/*
 * soyuz_rt.c — M:N cooperative task runtime (M-12 + M-13)
 *
 * Architecture:
 *   - Thread pool of N carrier threads (one per logical CPU)
 *   - Each task has its own 128 KB stack + ucontext_t          [M-12]
 *   - srt_await() yields the carrier thread cooperatively      [M-12]
 *   - Work-stealing: each worker has a Chase-Lev deque         [M-13]
 *     * Owner pushes/pops from the bottom (wait-free)
 *     * Stealers pop from the top (CAS)
 *     * External enqueues go into a shared injection queue
 *     * Worker: local pop → random steal → injection queue → sleep
 */

#include "soyuz_rt.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdatomic.h>
#include <unistd.h>
#include <time.h>

/* ── Stack size for each task coroutine ────────────────────────────────── */
#define SRT_STACK_SIZE   (128 * 1024)

/* ── Chase-Lev work-stealing deque ─────────────────────────────────────── */
/*
 * Each worker thread owns one deque.
 *   bottom: owner pushes here (increments bottom after write)
 *   top:    stealers pop from here (CAS to increment top)
 * Invariant: bottom - top == number of items.
 * capacity must be a power of 2.
 */

#define DEQUE_INIT_CAP 64   /* initial capacity (power of 2) */

typedef struct {
    _Atomic long     top;
    _Atomic long     bottom;
    srt_task_t     **buf;      /* circular buffer, length = capacity */
    long             capacity; /* current capacity (power of 2) */
} srt_deque_t;

static void deque_init(srt_deque_t *d) {
    d->buf      = calloc(DEQUE_INIT_CAP, sizeof(srt_task_t *));
    d->capacity = DEQUE_INIT_CAP;
    atomic_store_explicit(&d->top,    0, memory_order_relaxed);
    atomic_store_explicit(&d->bottom, 0, memory_order_relaxed);
}

static void deque_free(srt_deque_t *d) {
    free(d->buf);
    d->buf = NULL;
}

/* Grow the buffer (called only by the owner when full). */
static void deque_grow(srt_deque_t *d) {
    long t  = atomic_load_explicit(&d->top,    memory_order_relaxed);
    long b  = atomic_load_explicit(&d->bottom, memory_order_relaxed);
    long old_cap = d->capacity;
    long new_cap = old_cap * 2;
    srt_task_t **new_buf = calloc((size_t)new_cap, sizeof(srt_task_t *));
    for (long i = t; i < b; i++)
        new_buf[i & (new_cap - 1)] = d->buf[i & (old_cap - 1)];
    free(d->buf);
    d->buf      = new_buf;
    d->capacity = new_cap;
}

/* Push a task (owner only — no synchronisation needed on bottom write). */
static void deque_push(srt_deque_t *d, srt_task_t *task) {
    long b  = atomic_load_explicit(&d->bottom, memory_order_relaxed);
    long t  = atomic_load_explicit(&d->top,    memory_order_acquire);
    if (b - t >= d->capacity - 1)
        deque_grow(d);
    d->buf[b & (d->capacity - 1)] = task;
    atomic_thread_fence(memory_order_release);
    atomic_store_explicit(&d->bottom, b + 1, memory_order_relaxed);
}

/* Pop a task (owner only — may race with a single steal on last item). */
static srt_task_t *deque_pop(srt_deque_t *d) {
    long b = atomic_load_explicit(&d->bottom, memory_order_relaxed) - 1;
    atomic_store_explicit(&d->bottom, b, memory_order_relaxed);
    atomic_thread_fence(memory_order_seq_cst);
    long t = atomic_load_explicit(&d->top, memory_order_relaxed);

    if (t > b) {
        /* Deque was empty */
        atomic_store_explicit(&d->bottom, b + 1, memory_order_relaxed);
        return NULL;
    }

    srt_task_t *task = d->buf[b & (d->capacity - 1)];

    if (t == b) {
        /* Last item — race with potential stealer */
        long expected = t;
        if (!atomic_compare_exchange_strong_explicit(
                &d->top, &expected, t + 1,
                memory_order_seq_cst, memory_order_relaxed)) {
            /* Stealer won */
            task = NULL;
        }
        atomic_store_explicit(&d->bottom, b + 1, memory_order_relaxed);
    }
    return task;
}

/* Steal a task (any thread — steals from the top). */
static srt_task_t *deque_steal(srt_deque_t *d) {
    long t = atomic_load_explicit(&d->top,    memory_order_acquire);
    atomic_thread_fence(memory_order_seq_cst);
    long b = atomic_load_explicit(&d->bottom, memory_order_acquire);

    if (t >= b) return NULL; /* empty */

    srt_task_t *task = d->buf[t & (d->capacity - 1)];
    long expected = t;
    if (!atomic_compare_exchange_strong_explicit(
            &d->top, &expected, t + 1,
            memory_order_seq_cst, memory_order_relaxed)) {
        return NULL; /* lost CAS race */
    }
    return task;
}

/* ── Global injection queue (for srt_enqueue from non-worker contexts) ── */
/* Protected by g_inject_mu.  Workers drain this after exhausting local work. */

typedef struct srt_qnode {
    srt_task_t      *task;
    struct srt_qnode *next;
} srt_qnode_t;

typedef struct {
    srt_qnode_t *head;
    srt_qnode_t *tail;
    int          count;
} srt_queue_t;

static void inject_push(srt_queue_t *q, srt_task_t *t) {
    srt_qnode_t *n = malloc(sizeof(srt_qnode_t));
    n->task = t;
    n->next = NULL;
    if (q->tail) q->tail->next = n; else q->head = n;
    q->tail = n;
    q->count++;
}

static srt_task_t *inject_pop(srt_queue_t *q) {
    srt_qnode_t *n = q->head;
    if (!n) return NULL;
    q->head = n->next;
    if (!q->head) q->tail = NULL;
    q->count--;
    srt_task_t *t = n->task;
    free(n);
    return t;
}

/* ── Worker thread structure ────────────────────────────────────────────── */

typedef struct {
    pthread_t    tid;
    int          id;
    srt_deque_t  deque;
    srt_ctx_t    sched_ctx;  /* scheduler ucontext — heap-allocated to avoid stack adjacency with task */
} srt_worker_t;

typedef struct {
    srt_worker_t    *workers;
    int              n_workers;
    srt_queue_t      inject;       /* global injection queue */
    pthread_mutex_t  inject_mu;
    pthread_cond_t   cond;         /* signaled when new work arrives */
    _Atomic int      idle_count;   /* number of idle workers */
    int              shutdown;
    _Atomic int      waiting_tasks; /* tasks in SRT_WAITING (not in any deque) */
} srt_pool_t;

static srt_pool_t *g_pool    = NULL;

/* Thread-local: current task and scheduler context (M-12) */
__thread srt_task_t  *srt__current_task = NULL;
__thread srt_ctx_t   *srt__sched_ctx    = NULL;
/* Thread-local: pointer to this worker's deque (NULL on main thread) */
__thread srt_deque_t *srt__local_deque  = NULL;
/* Thread-local: this worker's index (for steal victim selection) */
__thread int          srt__worker_id    = -1;

/* ── Task reference counting ────────────────────────────────────────────── */

static void task_release(srt_task_t *t) {
    int32_t rc = __atomic_sub_fetch(&t->refcount, 1, __ATOMIC_ACQ_REL);
    if (rc == 0) {
        srt_child_node_t *node = t->children;
        while (node) {
            srt_child_node_t *next = node->next;
            free(node);
            node = next;
        }
        pthread_mutex_destroy(&t->children_mu);
        pthread_mutex_destroy(&t->waiters_mu);
        pthread_mutex_destroy(&t->mu);
        pthread_cond_destroy(&t->done_cond);
        if (t->stack) { free(t->stack); t->stack = NULL; }
        free(t);
    }
}

/* ── M-10 helpers ───────────────────────────────────────────────────────── */

static void unlink_from_parent(srt_task_t *t) {
    srt_task_t *parent = t->parent;
    if (!parent) return;
    pthread_mutex_lock(&parent->children_mu);
    srt_child_node_t **pp = &parent->children;
    while (*pp) {
        if ((*pp)->child == t) {
            srt_child_node_t *node = *pp;
            *pp = node->next;
            free(node);
            break;
        }
        pp = &(*pp)->next;
    }
    pthread_mutex_unlock(&parent->children_mu);
    t->parent = NULL;
}

/* ── M-13: task dispatch helpers ────────────────────────────────────────── */

/*
 * Dispatch a task to be run:
 *   - If on a worker thread → push to own deque (best locality)
 *   - Otherwise             → push to global injection queue
 * Then signal idle workers.
 */
static void dispatch_task(srt_task_t *t) {
    if (srt__local_deque) {
        deque_push(srt__local_deque, t);
    } else {
        pthread_mutex_lock(&g_pool->inject_mu);
        inject_push(&g_pool->inject, t);
        pthread_mutex_unlock(&g_pool->inject_mu);
    }
    /* Wake one idle worker */
    pthread_mutex_lock(&g_pool->inject_mu);
    pthread_cond_signal(&g_pool->cond);
    pthread_mutex_unlock(&g_pool->inject_mu);
}

/* ── M-12: wake coroutine waiters ───────────────────────────────────────── */

static void wake_waiters(srt_task_t *t) {
    pthread_mutex_lock(&t->waiters_mu);
    srt_task_t *w = t->waiters;
    t->waiters = NULL;
    pthread_mutex_unlock(&t->waiters_mu);

    while (w) {
        srt_task_t *next = w->waiter_next;
        w->waiter_next = NULL;
        /* Task is leaving SRT_WAITING — dequeue from waiting counter before
         * dispatching so shutdown never observes waiting_tasks > 0 with empty
         * deques and exits while this task is still outstanding. */
        if (g_pool)
            atomic_fetch_sub_explicit(&g_pool->waiting_tasks, 1, memory_order_release);
        dispatch_task(w);
        w = next;
    }
}

/* ── Task coroutine entry point ─────────────────────────────────────────── */

static void task_entry_trampoline(void) {
    srt_task_t *task = srt__current_task;
    task->fn(task->args);
    __atomic_store_n(&task->state, SRT_DONE, __ATOMIC_RELEASE);
    /* Explicitly swap back to the CURRENT worker's scheduler via task->sched_ctx.
     * We cannot rely on uc_link here: makecontext embeds the uc_link value on the
     * task's own stack, so updating uc_link in the struct after work-stealing has
     * no effect on where __start_context returns.  Using swapcontext ensures we
     * always land in the scheduler that owns THIS execution, not the original one. */
    srt_ctx_t *sched = task->sched_ctx;
    swapcontext(&task->coro_ctx.uc, &sched->uc);
    /* Unreachable */
    abort();
}

/* ── M-13: steal from a random peer ────────────────────────────────────── */

static srt_task_t *try_steal(int self_id) {
    int n = g_pool->n_workers;
    if (n <= 1) return NULL;
    /* Random starting victim (avoid always picking index 0) */
    int start = (int)((unsigned)rand() % (unsigned)(n - 1));
    if (start >= self_id) start++;
    for (int i = 0; i < n - 1; i++) {
        int victim = (start + i) % n;
        if (victim == self_id) { victim = (victim + 1) % n; }
        srt_task_t *t = deque_steal(&g_pool->workers[victim].deque);
        if (t) return t;
    }
    return NULL;
}

/* ── Worker loop ────────────────────────────────────────────────────────── */

static void *worker_thread(void *arg) {
    srt_worker_t *self = (srt_worker_t *)arg;
    srt__local_deque = &self->deque;
    srt__worker_id   = self->id;

    /* M-12: scheduler context lives in srt_worker_t (heap), not on this stack.
     * Keeping sched_ctx on the stack placed it adjacent to 'task', which caused
     * setcontext (called via uc_link after a task finishes) to restore Worker's
     * registers while another loop iteration had already written NULL to 'task'
     * at [rbp-offset]. Moving it to the heap eliminates that adjacency. */
    srt_ctx_t *const sched_ctx = &self->sched_ctx;
    memset(sched_ctx, 0, sizeof(*sched_ctx));
    srt__sched_ctx = sched_ctx;

    for (;;) {
        srt_task_t *task = NULL;

        /* 1. Pop from own deque */
        task = deque_pop(&self->deque);

        /* 2. Steal from a random peer */
        if (!task)
            task = try_steal(self->id);

        /* 3. Drain global injection queue */
        if (!task) {
            pthread_mutex_lock(&g_pool->inject_mu);
            task = inject_pop(&g_pool->inject);
            pthread_mutex_unlock(&g_pool->inject_mu);
        }

        /* 4. No work — check shutdown or sleep */
        if (!task) {
            pthread_mutex_lock(&g_pool->inject_mu);
            if (g_pool->shutdown) {
                /* Check all deques are empty AND no task is in SRT_WAITING
                 * before exiting.  Waiting tasks are not in any deque; they
                 * re-enter the deque only when their dependency completes via
                 * wake_waiters.  Exiting early while waiting_tasks > 0 would
                 * leave those tasks orphaned and cause a use-after-free when
                 * g_pool->workers is freed by srt_shutdown. */
                int all_empty = (g_pool->inject.count == 0) &&
                                (atomic_load_explicit(&g_pool->waiting_tasks,
                                                      memory_order_acquire) == 0);
                if (all_empty) {
                    for (int i = 0; i < g_pool->n_workers && all_empty; i++) {
                        long b = atomic_load_explicit(&g_pool->workers[i].deque.bottom,
                                                      memory_order_acquire);
                        long t = atomic_load_explicit(&g_pool->workers[i].deque.top,
                                                      memory_order_acquire);
                        if (b > t) all_empty = 0;
                    }
                }
                if (all_empty) {
                    pthread_mutex_unlock(&g_pool->inject_mu);
                    break;
                }
            }
            /* Sleep briefly, waiting for new work */
            struct timespec deadline;
            clock_gettime(CLOCK_REALTIME, &deadline);
            deadline.tv_nsec += 1000000L; /* 1 ms */
            if (deadline.tv_nsec >= 1000000000L) {
                deadline.tv_sec++;
                deadline.tv_nsec -= 1000000000L;
            }
            pthread_cond_timedwait(&g_pool->cond, &g_pool->inject_mu, &deadline);
            pthread_mutex_unlock(&g_pool->inject_mu);
            continue;
        }

        /* ── Run the task (M-12 coroutine) ────────────────────────────── */
        srt__current_task = task;
        __atomic_store_n(&task->state, SRT_RUNNING, __ATOMIC_RELEASE);

        /* Always record which worker is running this task so srt_await can
         * yield to the right scheduler context even after work-stealing
         * (the ucontext %fs base in coro_ctx is stale after migration). */
        task->sched_ctx = sched_ctx;

        if (!task->stack) {
            /* First time: allocate stack and set up ucontext */
            task->stack = malloc(SRT_STACK_SIZE);
            if (!task->stack) { fprintf(stderr, "Soyuz panic: stack alloc failed\n"); abort(); }
            getcontext(&task->coro_ctx.uc);
            task->coro_ctx.uc.uc_stack.ss_sp   = task->stack;
            task->coro_ctx.uc.uc_stack.ss_size = SRT_STACK_SIZE;
            task->coro_ctx.uc.uc_link          = NULL; /* unused: trampoline swaps back explicitly */
            makecontext(&task->coro_ctx.uc, task_entry_trampoline, 0);
        }

        swapcontext(&sched_ctx->uc, &task->coro_ctx.uc);

        /* ── Back in scheduler ─────────────────────────────────────────── */
        srt__current_task = NULL;

        /* The coroutine's coro_ctx is now fully saved.  If it held a mutex
         * across the yield (to prevent premature dispatch), unlock it now.
         * wake_waiters can only dispatch this task after the unlock. */
        if (task->pending_unlock) {
            pthread_mutex_unlock(task->pending_unlock);
            task->pending_unlock = NULL;
        }

        int32_t state = __atomic_load_n(&task->state, __ATOMIC_ACQUIRE);
        if (state == SRT_DONE || state == SRT_CANCELLED) {
            wake_waiters(task);
            pthread_mutex_lock(&task->mu);
            pthread_cond_broadcast(&task->done_cond);
            pthread_mutex_unlock(&task->mu);
            task_release(task);
        }
        /* SRT_WAITING: task will be re-dispatched by wake_waiters */
    }

    srt__sched_ctx   = NULL;
    srt__local_deque = NULL;
    srt__worker_id   = -1;
    return NULL;
}

/* ── Public API ─────────────────────────────────────────────────────────── */

void srt_init(int n_threads) {
    if (g_pool) return;
    if (n_threads <= 0) {
        n_threads = (int)sysconf(_SC_NPROCESSORS_ONLN);
        if (n_threads <= 0) n_threads = 4;
    }
    g_pool = calloc(1, sizeof(srt_pool_t));
    g_pool->n_workers = n_threads;
    g_pool->workers   = calloc((size_t)n_threads, sizeof(srt_worker_t));
    pthread_mutex_init(&g_pool->inject_mu, NULL);
    pthread_cond_init (&g_pool->cond,      NULL);

    for (int i = 0; i < n_threads; i++) {
        g_pool->workers[i].id = i;
        deque_init(&g_pool->workers[i].deque);
        pthread_create(&g_pool->workers[i].tid, NULL, worker_thread, &g_pool->workers[i]);
    }
}

void *srt_enqueue(void (*fn)(void *), void *args) {
    srt_task_t *t = calloc(1, sizeof(srt_task_t));
    t->fn       = fn;
    t->args     = args;
    t->state    = SRT_PENDING;
    t->detached = 0;
    __atomic_store_n(&t->refcount, 2, __ATOMIC_RELAXED);
    pthread_mutex_init(&t->mu,          NULL);
    pthread_cond_init (&t->done_cond,   NULL);
    pthread_mutex_init(&t->children_mu, NULL);
    pthread_mutex_init(&t->waiters_mu,  NULL);
    t->children       = NULL;
    t->parent         = NULL;
    t->stack          = NULL;
    t->waiters        = NULL;
    t->waiter_next    = NULL;
    t->pending_unlock = NULL;

    /* M-10: link as child of currently-running task */
    srt_task_t *parent = srt__current_task;
    if (parent) {
        t->parent = parent;
        srt_child_node_t *node = malloc(sizeof(srt_child_node_t));
        node->child = t;
        node->next  = NULL;
        pthread_mutex_lock(&parent->children_mu);
        node->next       = parent->children;
        parent->children = node;
        pthread_mutex_unlock(&parent->children_mu);
    }

    dispatch_task(t);
    return (void *)t;
}

void *srt_await(void *handle) {
    srt_task_t *t = (srt_task_t *)handle;

    /* Fast path: already done.
     * NOTE: use srt_finished() not ">= SRT_DONE" — SRT_WAITING=4 > SRT_DONE=2
     * and would falsely match, causing callers to read result=0 prematurely. */
    if (srt_finished(__atomic_load_n(&t->state, __ATOMIC_ACQUIRE))) {
        void *result = t->result;
        unlink_from_parent(t);
        task_release(t);
        return result;
    }

    srt_task_t *self = srt__current_task;
    if (self && self->sched_ctx) {
        /* Coroutine path: add self to t's waiter list and yield.
         * Use self->sched_ctx (set by the worker that dispatched this task)
         * instead of the thread-local srt__sched_ctx.  After work-stealing
         * the %fs TLS base in coro_ctx may belong to a different worker, so
         * the thread-local would resolve incorrectly. */
        srt_ctx_t *my_sched = self->sched_ctx;
        pthread_mutex_lock(&t->waiters_mu);
        if (srt_finished(__atomic_load_n(&t->state, __ATOMIC_ACQUIRE))) {
            pthread_mutex_unlock(&t->waiters_mu);
        } else {
            self->waiter_next = t->waiters;
            t->waiters        = self;
            __atomic_store_n(&self->state, SRT_WAITING, __ATOMIC_RELEASE);
            /* Track this task as waiting so shutdown doesn't exit prematurely. */
            if (g_pool)
                atomic_fetch_add_explicit(&g_pool->waiting_tasks, 1, memory_order_relaxed);
            /* Keep t->waiters_mu LOCKED across swapcontext.
             * The scheduler will unlock it AFTER swapcontext saves our coro_ctx.
             * If we unlocked here, wake_waiters could dispatch us before our
             * coroutine context is fully saved, causing two workers to race on
             * the same stack (the premature-dispatch race). */
            self->pending_unlock = &t->waiters_mu;
            swapcontext(&self->coro_ctx.uc, &my_sched->uc);
            /* Resumed: t is now DONE or CANCELLED.
             * t->waiters_mu was already unlocked by the scheduler.
             * Acquire fence: pairs with the RELEASE in task_entry_trampoline
             * (t->state = SRT_DONE) so that t->result is visible here.
             * The deque/queue ops in dispatch_task provide ordering from the
             * scheduler's perspective but not from this coroutine's perspective
             * after a context switch.  An explicit fence closes the gap. */
            atomic_thread_fence(memory_order_acquire);
        }
    } else {
        /* Blocking path: main thread or non-task context */
        pthread_mutex_lock(&t->mu);
        while (!srt_finished(__atomic_load_n(&t->state, __ATOMIC_ACQUIRE)))
            pthread_cond_wait(&t->done_cond, &t->mu);
        pthread_mutex_unlock(&t->mu);
    }

    void *result = t->result;
    unlink_from_parent(t);
    task_release(t);
    return result;
}

void srt_set_task_result(void *result) {
    srt_task_t *t = srt__current_task;
    if (t) t->result = result;
}

void srt_detach(void *handle) {
    srt_task_t *t = (srt_task_t *)handle;
    unlink_from_parent(t);
    __atomic_store_n(&t->detached, 1, __ATOMIC_RELEASE);
    task_release(t);
}

void srt_cancel(void *handle) {
    if (!handle) return;
    srt_task_t *t = (srt_task_t *)handle;
    __atomic_store_n(&t->state, SRT_CANCELLED, __ATOMIC_RELEASE);

    wake_waiters(t);

    pthread_mutex_lock(&t->mu);
    pthread_cond_broadcast(&t->done_cond);
    pthread_mutex_unlock(&t->mu);

    pthread_mutex_lock(&t->children_mu);
    int n = 0;
    for (srt_child_node_t *nd = t->children; nd; nd = nd->next) n++;
    srt_task_t **arr = (n > 0) ? malloc(n * sizeof(srt_task_t *)) : NULL;
    int i = 0;
    for (srt_child_node_t *nd = t->children; nd; nd = nd->next) {
        arr[i] = nd->child;
        __atomic_add_fetch(&nd->child->refcount, 1, __ATOMIC_ACQ_REL);
        i++;
    }
    pthread_mutex_unlock(&t->children_mu);

    for (i = 0; i < n; i++) {
        srt_cancel(arr[i]);
        task_release(arr[i]);
    }
    free(arr);
}

void srt_drop_task_handle(void *handle) {
    if (!handle) return;
    srt_task_t *t = (srt_task_t *)handle;
    int32_t st = __atomic_load_n(&t->state, __ATOMIC_ACQUIRE);
    if (st != SRT_DONE && st != SRT_CANCELLED) {
        fprintf(stderr, "Soyuz panic: Task nao consumida — use .await() ou .detach()\n");
        abort();
    }
    pthread_mutex_lock(&t->children_mu);
    for (srt_child_node_t *nd = t->children; nd; nd = nd->next) {
        int32_t cs = __atomic_load_n(&nd->child->state, __ATOMIC_ACQUIRE);
        if (cs == SRT_PENDING || cs == SRT_RUNNING || cs == SRT_WAITING) {
            pthread_mutex_unlock(&t->children_mu);
            fprintf(stderr,
                "Soyuz panic: Task encerrada com filhos ainda executando "
                "— chame .await() ou .detach() nos filhos\n");
            abort();
        }
    }
    pthread_mutex_unlock(&t->children_mu);
    unlink_from_parent(t);
    task_release(t);
}

void *srt_await_any(void **handles, int n) {
    if (n <= 0) return NULL;
    for (;;) {
        for (int i = 0; i < n; i++) {
            srt_task_t *t = (srt_task_t *)handles[i];
            if (!t) continue;
            pthread_mutex_lock(&t->mu);
            int state = __atomic_load_n(&t->state, __ATOMIC_ACQUIRE);
            if (state == SRT_DONE) {
                void *result = t->result;
                pthread_mutex_unlock(&t->mu);
                unlink_from_parent(t);
                task_release(t);
                handles[i] = NULL;
                for (int j = 0; j < n; j++) {
                    if (handles[j]) { srt_detach(handles[j]); handles[j] = NULL; }
                }
                return result;
            }
            struct timespec dl;
            clock_gettime(CLOCK_REALTIME, &dl);
            long ns = dl.tv_nsec + 500000L;
            if (ns >= 1000000000L) { dl.tv_sec++; dl.tv_nsec = ns - 1000000000L; }
            else dl.tv_nsec = ns;
            pthread_cond_timedwait(&t->done_cond, &t->mu, &dl);
            pthread_mutex_unlock(&t->mu);
        }
    }
}

void srt_shutdown(void) {
    if (!g_pool) return;
    pthread_mutex_lock(&g_pool->inject_mu);
    g_pool->shutdown = 1;
    pthread_cond_broadcast(&g_pool->cond);
    pthread_mutex_unlock(&g_pool->inject_mu);
    for (int i = 0; i < g_pool->n_workers; i++)
        pthread_join(g_pool->workers[i].tid, NULL);
    for (int i = 0; i < g_pool->n_workers; i++)
        deque_free(&g_pool->workers[i].deque);
    pthread_mutex_destroy(&g_pool->inject_mu);
    pthread_cond_destroy (&g_pool->cond);
    free(g_pool->workers);
    free(g_pool);
    g_pool = NULL;
}

srt_task_t *srt_current_task(void)       { return srt__current_task; }
void       *srt_task_handle_current(void) { return (void *)srt__current_task; }

int srt_task_cancelled(void *handle) {
    if (!handle) return 0;
    srt_task_t *t = (srt_task_t *)handle;
    return __atomic_load_n(&t->state, __ATOMIC_ACQUIRE) == SRT_CANCELLED ? 1 : 0;
}

void srt_task_set_progress(void *handle, double progress) {
    if (!handle) return;
    ((srt_task_t *)handle)->progress = progress;
}

__attribute__((constructor)) static void srt_auto_init(void)     { srt_init(0); }
__attribute__((destructor))  static void srt_auto_shutdown(void) { srt_shutdown(); }

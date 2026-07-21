/*
 * std_arc.c — Arc[T] com Epoch-Based Reclamation (EBR) — M-14
 *
 * Arc[T] é um ponteiro com contagem de referências atômica seguro para
 * compartilhamento entre tasks/threads. Quando o refcount chega a zero,
 * o objeto não é liberado imediatamente: ele é "aposentado" (retired) na
 * lista do epoch atual e liberado apenas quando todas as threads ativas
 * avançaram além do epoch de aposentadoria.
 *
 * Algoritmo EBR (3 épocas, rotativas):
 *   - global_epoch: 0, 1 ou 2 (avança ciclicamente)
 *   - Cada thread registrada anuncia seu local_epoch (EBR_INACTIVE = idle)
 *   - arc_pin()/arc_unpin(): anuncia/remove anúncio do epoch atual
 *   - srt_arc_release(): se refcount → 0, retira no epoch atual
 *   - srt_arc_try_advance(): avança global_epoch se todos os threads
 *     anunciaram >= epoch atual; libera a retire list do epoch mais velho
 *
 * Thread registration:
 *   - Automática via pthread TLS key (ao primeiro uso de Arc em cada thread)
 *   - Deregistração automática pelo destrutor TLS ao sair do thread
 */

#include <stdint.h>
#include <stdatomic.h>
#include <stdlib.h>
#include <stdio.h>
#include <pthread.h>
#include <string.h>
#include <time.h>

/* ── EBR constants ──────────────────────────────────────────────────────── */
#define EBR_N_EPOCHS    3
#define EBR_MAX_THREADS 128
#define EBR_INACTIVE    UINT64_MAX
#define EBR_RETIRE_BATCH 32   /* try to advance every N retires */

/* ── Per-thread slot in the global registry ─────────────────────────────── */
typedef struct {
    _Atomic uint64_t local_epoch; /* EBR_INACTIVE or current epoch */
    _Atomic int      active;      /* 1 = this slot is in use */
} ebr_slot_t;

static ebr_slot_t        g_slots[EBR_MAX_THREADS];
static _Atomic uint64_t  g_epoch = 0;   /* global epoch (0, 1, 2, ...) */
static pthread_mutex_t   g_mu    = PTHREAD_MUTEX_INITIALIZER;

/* ── Per-thread state (TLS) ─────────────────────────────────────────────── */
typedef struct ebr_retired {
    void *ptr;
    struct ebr_retired *next;
} ebr_retired_t;

static __thread int           tl_slot        = -1;
static __thread ebr_retired_t *tl_retire[EBR_N_EPOCHS]; /* retire lists */
static __thread size_t         tl_retire_cnt = 0;

/* ── TLS key for auto-deregistration ────────────────────────────────────── */
static pthread_key_t    g_tls_key;
static pthread_once_t   g_tls_once = PTHREAD_ONCE_INIT;

static void ebr_thread_deregister(void *arg) {
    (void)arg;
    if (tl_slot < 0) return;
    /* Clear local epoch and mark slot inactive */
    atomic_store_explicit(&g_slots[tl_slot].local_epoch,
                          EBR_INACTIVE, memory_order_release);
    atomic_store_explicit(&g_slots[tl_slot].active, 0, memory_order_release);
    tl_slot = -1;
    /* Free any remaining retired objects (safe: thread is exiting) */
    for (int e = 0; e < EBR_N_EPOCHS; e++) {
        ebr_retired_t *r = tl_retire[e];
        while (r) {
            ebr_retired_t *next = r->next;
            free(r->ptr);
            free(r);
            r = next;
        }
        tl_retire[e] = NULL;
    }
}

static void ebr_tls_init(void) {
    pthread_key_create(&g_tls_key, ebr_thread_deregister);
}

/* Register the current thread. Idempotent. */
static void ebr_thread_register(void) {
    if (tl_slot >= 0) return;
    pthread_once(&g_tls_once, ebr_tls_init);
    /* Set a non-NULL value for the TLS key so the destructor fires on exit */
    pthread_setspecific(g_tls_key, (void *)1);

    pthread_mutex_lock(&g_mu);
    for (int i = 0; i < EBR_MAX_THREADS; i++) {
        int expected = 0;
        if (atomic_compare_exchange_strong(&g_slots[i].active, &expected, 1)) {
            tl_slot = i;
            atomic_store_explicit(&g_slots[i].local_epoch,
                                  EBR_INACTIVE, memory_order_relaxed);
            break;
        }
    }
    pthread_mutex_unlock(&g_mu);

    if (tl_slot < 0) {
        fprintf(stderr, "Soyuz Arc: máximo de threads (%d) excedido\n", EBR_MAX_THREADS);
        abort();
    }
}

/* ── Epoch pin / unpin ──────────────────────────────────────────────────── */

static void ebr_pin(void) {
    ebr_thread_register();
    uint64_t e = atomic_load_explicit(&g_epoch, memory_order_acquire);
    atomic_store_explicit(&g_slots[tl_slot].local_epoch, e, memory_order_release);
    /* Re-read to handle racing advance */
    atomic_thread_fence(memory_order_seq_cst);
}

static void ebr_unpin(void) {
    if (tl_slot < 0) return;
    atomic_store_explicit(&g_slots[tl_slot].local_epoch,
                          EBR_INACTIVE, memory_order_release);
}

/* ── Epoch advancement and reclamation ──────────────────────────────────── */

static void ebr_try_advance(void) {
    uint64_t cur = atomic_load_explicit(&g_epoch, memory_order_acquire);

    /* Check if all active threads have announced cur or are idle */
    for (int i = 0; i < EBR_MAX_THREADS; i++) {
        if (!atomic_load_explicit(&g_slots[i].active, memory_order_acquire))
            continue;
        uint64_t le = atomic_load_explicit(&g_slots[i].local_epoch,
                                           memory_order_acquire);
        if (le != EBR_INACTIVE && le < cur) {
            return; /* some thread is still at an old epoch */
        }
    }

    /* Safe to advance */
    uint64_t next = (cur + 1) % EBR_N_EPOCHS;
    uint64_t expected = cur;
    if (!atomic_compare_exchange_strong_explicit(
            &g_epoch, &expected, next,
            memory_order_acq_rel, memory_order_relaxed)) {
        return; /* another thread advanced it */
    }

    /* Reclaim the oldest epoch's retire list (2 epochs ago = safe) */
    uint64_t old = (next + 1) % EBR_N_EPOCHS; /* == (cur - 1 + 3) % 3 */
    ebr_retired_t *r = tl_retire[old];
    tl_retire[old] = NULL;
    while (r) {
        ebr_retired_t *nxt = r->next;
        free(r->ptr);
        free(r);
        r = nxt;
    }
}

/* Retire an object into the current epoch's retire list. */
static void ebr_retire(void *ptr) {
    ebr_thread_register();
    ebr_retired_t *node = malloc(sizeof(ebr_retired_t));
    node->ptr  = ptr;
    node->next = NULL;

    uint64_t e = atomic_load_explicit(&g_epoch, memory_order_acquire) % EBR_N_EPOCHS;
    node->next    = tl_retire[e];
    tl_retire[e]  = node;
    tl_retire_cnt++;

    if (tl_retire_cnt >= EBR_RETIRE_BATCH) {
        tl_retire_cnt = 0;
        ebr_try_advance();
    }
}

/* ── Arc header ─────────────────────────────────────────────────────────── */

typedef struct {
    int64_t refcount;        /* modified with __atomic_* via plain int64_t */
    int64_t value;           /* i64 payload (fits pointers too via bit cast) */
} srt_arc_t;

/* ── Public API ─────────────────────────────────────────────────────────── */

/*
 * srt_arc_new(value) → i8*
 * Creates a new Arc wrapping an i64 value. Refcount = 1.
 */
void *srt_arc_new(int64_t value) {
    ebr_thread_register();
    srt_arc_t *a = malloc(sizeof(srt_arc_t));
    __atomic_store_n(&a->refcount, 1, __ATOMIC_RELAXED);
    a->value = value;
    return (void *)a;
}

/*
 * srt_arc_clone(ptr) → i8*
 * Increment refcount and return the same pointer (another owner).
 */
void *srt_arc_clone(void *ptr) {
    srt_arc_t *a = (srt_arc_t *)ptr;
    __atomic_add_fetch(&a->refcount, 1, __ATOMIC_ACQ_REL);
    return ptr;
}

/*
 * srt_arc_release(ptr)
 * Decrement refcount. If it reaches 0, retire via EBR (deferred free).
 */
void srt_arc_release(void *ptr) {
    if (!ptr) return;
    srt_arc_t *a = (srt_arc_t *)ptr;
    int64_t rc = __atomic_sub_fetch(&a->refcount, 1, __ATOMIC_ACQ_REL);
    if (rc == 0) {
        ebr_retire(ptr);   /* deferred free via EBR */
    }
}

/*
 * srt_arc_get(ptr) → i64
 * Reads the inner value under epoch protection.
 */
int64_t srt_arc_get(void *ptr) {
    ebr_pin();
    srt_arc_t *a = (srt_arc_t *)ptr;
    int64_t val = __atomic_load_n(&a->value, __ATOMIC_ACQUIRE);
    ebr_unpin();
    return val;
}

/*
 * srt_arc_refcount(ptr) → i64
 * Returns the current reference count (for testing/debugging).
 */
int64_t srt_arc_refcount(void *ptr) {
    if (!ptr) return 0;
    srt_arc_t *a = (srt_arc_t *)ptr;
    return (int64_t)__atomic_load_n(&a->refcount, __ATOMIC_ACQUIRE);
}

/*
 * srt_arc_quiescent()
 * Called by worker threads between tasks (natural quiescent point).
 * Allows the EBR to advance and reclaim retired objects.
 */
void srt_arc_quiescent(void) {
    if (tl_slot < 0) return;
    atomic_store_explicit(&g_slots[tl_slot].local_epoch,
                          EBR_INACTIVE, memory_order_release);
    ebr_try_advance();
}

#include <stdint.h>
#include <stdlib.h>
#include <pthread.h>

/* ------------------------------------------------------------------ */
/* Mutex[T] — wraps pthread_mutex_t + int64_t data                    */
/* ------------------------------------------------------------------ */

typedef struct {
    int64_t         data;
    pthread_mutex_t mu;
} srt_mutex_t;

typedef struct {
    srt_mutex_t *mutex;
} srt_mutex_guard_t;

void *srt_mutex_new(int64_t initial) {
    srt_mutex_t *m = malloc(sizeof(srt_mutex_t));
    m->data = initial;
    pthread_mutex_init(&m->mu, NULL);
    return m;
}

void *srt_mutex_lock(void *mx) {
    srt_mutex_t *m = (srt_mutex_t *)mx;
    pthread_mutex_lock(&m->mu);
    srt_mutex_guard_t *g = malloc(sizeof(srt_mutex_guard_t));
    g->mutex = m;
    return g;
}

void srt_mutex_unlock(void *guard) {
    if (!guard) return;
    srt_mutex_guard_t *g = (srt_mutex_guard_t *)guard;
    pthread_mutex_unlock(&g->mutex->mu);
    free(g);
}

int64_t srt_mutex_guard_get(void *guard) {
    srt_mutex_guard_t *g = (srt_mutex_guard_t *)guard;
    return g->mutex->data;
}

void srt_mutex_guard_set(void *guard, int64_t val) {
    srt_mutex_guard_t *g = (srt_mutex_guard_t *)guard;
    g->mutex->data = val;
}

/* ------------------------------------------------------------------ */
/* RwLock[T] — wraps pthread_rwlock_t + int64_t data                  */
/* ------------------------------------------------------------------ */

typedef struct {
    int64_t          data;
    pthread_rwlock_t rw;
} srt_rwlock_t;

typedef struct {
    srt_rwlock_t *rwlock;
    int           is_write;
} srt_rw_guard_t;

void *srt_rwlock_new(int64_t initial) {
    srt_rwlock_t *r = malloc(sizeof(srt_rwlock_t));
    r->data = initial;
    pthread_rwlock_init(&r->rw, NULL);
    return r;
}

void *srt_rwlock_read(void *rw) {
    srt_rwlock_t *r = (srt_rwlock_t *)rw;
    pthread_rwlock_rdlock(&r->rw);
    srt_rw_guard_t *g = malloc(sizeof(srt_rw_guard_t));
    g->rwlock   = r;
    g->is_write = 0;
    return g;
}

void *srt_rwlock_write(void *rw) {
    srt_rwlock_t *r = (srt_rwlock_t *)rw;
    pthread_rwlock_wrlock(&r->rw);
    srt_rw_guard_t *g = malloc(sizeof(srt_rw_guard_t));
    g->rwlock   = r;
    g->is_write = 1;
    return g;
}

void srt_rwlock_unlock(void *guard) {
    if (!guard) return;
    srt_rw_guard_t *g = (srt_rw_guard_t *)guard;
    pthread_rwlock_unlock(&g->rwlock->rw);
    free(g);
}

int64_t srt_rwlock_guard_get(void *guard) {
    srt_rw_guard_t *g = (srt_rw_guard_t *)guard;
    return g->rwlock->data;
}

void srt_rwlock_guard_set(void *guard, int64_t val) {
    srt_rw_guard_t *g = (srt_rw_guard_t *)guard;
    g->rwlock->data = val;
}

/* ------------------------------------------------------------------ */
/* Atomic[T] — lock-free int64_t via GCC __atomic builtins            */
/* ------------------------------------------------------------------ */

typedef struct {
    volatile int64_t val;
} srt_atomic_t;

void *srt_atomic_new(int64_t initial) {
    srt_atomic_t *a = malloc(sizeof(srt_atomic_t));
    __atomic_store_n(&a->val, initial, __ATOMIC_SEQ_CST);
    return a;
}

int64_t srt_atomic_load(void *a) {
    return __atomic_load_n(&((srt_atomic_t *)a)->val, __ATOMIC_SEQ_CST);
}

void srt_atomic_store(void *a, int64_t val) {
    __atomic_store_n(&((srt_atomic_t *)a)->val, val, __ATOMIC_SEQ_CST);
}

int64_t srt_atomic_add(void *a, int64_t delta) {
    return __atomic_fetch_add(&((srt_atomic_t *)a)->val, delta, __ATOMIC_SEQ_CST);
}

/* Returns 1 on success (value was expected and was swapped), 0 on failure. */
int64_t srt_atomic_cas(void *a, int64_t expected, int64_t desired) {
    int64_t exp = expected;
    return __atomic_compare_exchange_n(
        &((srt_atomic_t *)a)->val, &exp, desired,
        0, __ATOMIC_SEQ_CST, __ATOMIC_SEQ_CST) ? 1 : 0;
}

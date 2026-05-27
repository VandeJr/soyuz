#include <stdio.h>
#include <stdlib.h>
#include <stdint.h>
#include <pthread.h>

// ── Channel[T] — bounded MPSC ring-buffer ─────────────────────────────────

typedef struct {
    int64_t        *buf;
    int64_t         capacity;
    int64_t         head;   // next write index
    int64_t         tail;   // next read index
    int64_t         count;
    int             closed;
    pthread_mutex_t mu;
    pthread_cond_t  not_empty;
    pthread_cond_t  not_full;
} srt_chan_t;

void *srt_chan_new(int64_t capacity) {
    srt_chan_t *ch = (srt_chan_t *)malloc(sizeof(srt_chan_t));
    ch->buf      = (int64_t *)malloc(sizeof(int64_t) * capacity);
    ch->capacity = capacity;
    ch->head     = 0;
    ch->tail     = 0;
    ch->count    = 0;
    ch->closed   = 0;
    pthread_mutex_init(&ch->mu, NULL);
    pthread_cond_init(&ch->not_empty, NULL);
    pthread_cond_init(&ch->not_full, NULL);
    return ch;
}

// Blocks if the buffer is full. Drops the send if the channel is closed.
void srt_chan_send(void *ch_ptr, int64_t val) {
    srt_chan_t *ch = (srt_chan_t *)ch_ptr;
    pthread_mutex_lock(&ch->mu);
    while (ch->count == ch->capacity && !ch->closed) {
        pthread_cond_wait(&ch->not_full, &ch->mu);
    }
    if (!ch->closed) {
        ch->buf[ch->head] = val;
        ch->head = (ch->head + 1) % ch->capacity;
        ch->count++;
        pthread_cond_signal(&ch->not_empty);
    }
    pthread_mutex_unlock(&ch->mu);
}

// Blocks until a value is available or the channel is closed and empty.
// Returns 1 and writes to *out on success; returns 0 if closed-and-empty.
int64_t srt_chan_recv(void *ch_ptr, int64_t *out) {
    srt_chan_t *ch = (srt_chan_t *)ch_ptr;
    pthread_mutex_lock(&ch->mu);
    while (ch->count == 0 && !ch->closed) {
        pthread_cond_wait(&ch->not_empty, &ch->mu);
    }
    if (ch->count > 0) {
        *out  = ch->buf[ch->tail];
        ch->tail  = (ch->tail + 1) % ch->capacity;
        ch->count--;
        pthread_cond_signal(&ch->not_full);
        pthread_mutex_unlock(&ch->mu);
        return 1;
    }
    pthread_mutex_unlock(&ch->mu);
    return 0;
}

// Non-blocking: returns 1 and writes to *out if a value is immediately available,
// 0 otherwise (empty or closed-and-empty).
int64_t srt_chan_try_recv(void *ch_ptr, int64_t *out) {
    srt_chan_t *ch = (srt_chan_t *)ch_ptr;
    pthread_mutex_lock(&ch->mu);
    if (ch->count > 0) {
        *out  = ch->buf[ch->tail];
        ch->tail  = (ch->tail + 1) % ch->capacity;
        ch->count--;
        pthread_cond_signal(&ch->not_full);
        pthread_mutex_unlock(&ch->mu);
        return 1;
    }
    pthread_mutex_unlock(&ch->mu);
    return 0;
}

void srt_chan_close(void *ch_ptr) {
    srt_chan_t *ch = (srt_chan_t *)ch_ptr;
    pthread_mutex_lock(&ch->mu);
    ch->closed = 1;
    pthread_cond_broadcast(&ch->not_empty);
    pthread_cond_broadcast(&ch->not_full);
    pthread_mutex_unlock(&ch->mu);
}

int64_t srt_chan_is_closed(void *ch_ptr) {
    srt_chan_t *ch = (srt_chan_t *)ch_ptr;
    pthread_mutex_lock(&ch->mu);
    int64_t c = ch->closed;
    pthread_mutex_unlock(&ch->mu);
    return c;
}

// ── SyncChannel[T] — rendezvous (capacity-0) channel ─────────────────────

typedef struct {
    int64_t         value;
    int             has_value;  // sender deposited
    int             acked;      // receiver took value
    int             closed;
    pthread_mutex_t mu;
    pthread_cond_t  sender_go;   // sender waits here; receiver signals
    pthread_cond_t  receiver_go; // receiver waits here; sender signals
} srt_sync_chan_t;

void *srt_sync_chan_new(void) {
    srt_sync_chan_t *sc = (srt_sync_chan_t *)malloc(sizeof(srt_sync_chan_t));
    sc->has_value = 0;
    sc->acked     = 0;
    sc->closed    = 0;
    pthread_mutex_init(&sc->mu, NULL);
    pthread_cond_init(&sc->sender_go, NULL);
    pthread_cond_init(&sc->receiver_go, NULL);
    return sc;
}

// Blocks until the receiver takes the value (rendezvous).
void srt_sync_chan_send(void *sc_ptr, int64_t val) {
    srt_sync_chan_t *sc = (srt_sync_chan_t *)sc_ptr;
    pthread_mutex_lock(&sc->mu);
    while (sc->has_value && !sc->closed) {
        pthread_cond_wait(&sc->sender_go, &sc->mu);
    }
    if (!sc->closed) {
        sc->value     = val;
        sc->has_value = 1;
        sc->acked     = 0;
        pthread_cond_signal(&sc->receiver_go);
        while (!sc->acked && !sc->closed) {
            pthread_cond_wait(&sc->sender_go, &sc->mu);
        }
        sc->has_value = 0;
    }
    pthread_mutex_unlock(&sc->mu);
}

// Blocks until a sender arrives. Returns 1 + value in *out; 0 if closed.
int64_t srt_sync_chan_recv(void *sc_ptr, int64_t *out) {
    srt_sync_chan_t *sc = (srt_sync_chan_t *)sc_ptr;
    pthread_mutex_lock(&sc->mu);
    while (!sc->has_value && !sc->closed) {
        pthread_cond_wait(&sc->receiver_go, &sc->mu);
    }
    if (sc->has_value) {
        *out      = sc->value;
        sc->acked = 1;
        pthread_cond_signal(&sc->sender_go);
        pthread_mutex_unlock(&sc->mu);
        return 1;
    }
    pthread_mutex_unlock(&sc->mu);
    return 0;
}

void srt_sync_chan_close(void *sc_ptr) {
    srt_sync_chan_t *sc = (srt_sync_chan_t *)sc_ptr;
    pthread_mutex_lock(&sc->mu);
    sc->closed = 1;
    pthread_cond_broadcast(&sc->sender_go);
    pthread_cond_broadcast(&sc->receiver_go);
    pthread_mutex_unlock(&sc->mu);
}

// ── M-20: select { ch.recv() => body, default => body } ──────────────────────

// srt_select_try: non-blocking poll over n channels.
// On first channel that has a value, writes it to *out and returns its index.
// Returns -1 if no channel is ready.
int64_t srt_select_try(void **channels, int64_t n, int64_t *out) {
    for (int64_t i = 0; i < n; i++) {
        int64_t val;
        if (srt_chan_try_recv(channels[i], &val)) {
            *out = val;
            return i;
        }
    }
    return -1;
}

// srt_select: blocking select over n channels.
// Polls with exponential-backoff until any channel has a value.
// Returns the index of the ready channel and writes the value to *out.
int64_t srt_select(void **channels, int64_t n, int64_t *out) {
    struct timespec ts;
    long delay_ns = 1000; /* start: 1 µs */
    while (1) {
        int64_t idx = srt_select_try(channels, n, out);
        if (idx >= 0) return idx;
        ts.tv_sec  = 0;
        ts.tv_nsec = delay_ns;
        nanosleep(&ts, NULL);
        if (delay_ns < 1000000) delay_ns *= 2; /* cap at 1 ms */
    }
}

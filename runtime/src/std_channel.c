#include <stdio.h>
#include <stdlib.h>
#include <stdint.h>
#include <pthread.h>

// ── Channel[T] — unified channel (buffered N>0 or rendezvous N=0) ────────────
//
// capacity == 0: rendezvous semantics (sender blocks until receiver takes)
// capacity  > 0: bounded ring-buffer (sender blocks only when full)

typedef struct {
    int64_t        *buf;
    int64_t         capacity;  // 0 = rendezvous, N>0 = buffered
    int64_t         head;      // next write index (buffered mode)
    int64_t         tail;      // next read index  (buffered mode)
    int64_t         count;     // items in buffer  (buffered mode)
    int             closed;
    // rendezvous extras (used when capacity == 0)
    int64_t         rv_value;
    int             rv_has_value;
    int             rv_acked;
    pthread_mutex_t mu;
    pthread_cond_t  not_empty;    // buffered: data available; rendezvous: rv_has_value
    pthread_cond_t  not_full;     // buffered: space available
    pthread_cond_t  rv_sender_go; // rendezvous: sender waits for ack
} srt_chan_t;

void *srt_chan_new(int64_t capacity) {
    srt_chan_t *ch = (srt_chan_t *)malloc(sizeof(srt_chan_t));
    ch->buf          = capacity > 0 ? (int64_t *)malloc(sizeof(int64_t) * capacity) : NULL;
    ch->capacity     = capacity;
    ch->head         = 0;
    ch->tail         = 0;
    ch->count        = 0;
    ch->closed       = 0;
    ch->rv_value     = 0;
    ch->rv_has_value = 0;
    ch->rv_acked     = 0;
    pthread_mutex_init(&ch->mu, NULL);
    pthread_cond_init(&ch->not_empty, NULL);
    pthread_cond_init(&ch->not_full, NULL);
    pthread_cond_init(&ch->rv_sender_go, NULL);
    return ch;
}

// Blocks if the buffer is full (buffered) or until receiver takes (rendezvous).
void srt_chan_send(void *ch_ptr, int64_t val) {
    srt_chan_t *ch = (srt_chan_t *)ch_ptr;
    pthread_mutex_lock(&ch->mu);
    if (ch->capacity == 0) {
        // rendezvous: wait until no pending value and not closed
        while (ch->rv_has_value && !ch->closed)
            pthread_cond_wait(&ch->rv_sender_go, &ch->mu);
        if (!ch->closed) {
            ch->rv_value     = val;
            ch->rv_has_value = 1;
            ch->rv_acked     = 0;
            pthread_cond_signal(&ch->not_empty);     // wake receiver
            while (!ch->rv_acked && !ch->closed)
                pthread_cond_wait(&ch->rv_sender_go, &ch->mu);
            ch->rv_has_value = 0;
        }
    } else {
        while (ch->count == ch->capacity && !ch->closed)
            pthread_cond_wait(&ch->not_full, &ch->mu);
        if (!ch->closed) {
            ch->buf[ch->head] = val;
            ch->head = (ch->head + 1) % ch->capacity;
            ch->count++;
            pthread_cond_signal(&ch->not_empty);
        }
    }
    pthread_mutex_unlock(&ch->mu);
}

// Blocks until a value is available or the channel is closed and empty.
// Returns 1 + writes to *out on success; returns 0 if closed-and-empty.
int64_t srt_chan_recv(void *ch_ptr, int64_t *out) {
    srt_chan_t *ch = (srt_chan_t *)ch_ptr;
    pthread_mutex_lock(&ch->mu);
    if (ch->capacity == 0) {
        // rendezvous: wait until sender deposits a value
        while (!ch->rv_has_value && !ch->closed)
            pthread_cond_wait(&ch->not_empty, &ch->mu);
        if (ch->rv_has_value) {
            *out         = ch->rv_value;
            ch->rv_acked = 1;
            pthread_cond_signal(&ch->rv_sender_go);  // unblock sender
            pthread_mutex_unlock(&ch->mu);
            return 1;
        }
    } else {
        while (ch->count == 0 && !ch->closed)
            pthread_cond_wait(&ch->not_empty, &ch->mu);
        if (ch->count > 0) {
            *out      = ch->buf[ch->tail];
            ch->tail  = (ch->tail + 1) % ch->capacity;
            ch->count--;
            pthread_cond_signal(&ch->not_full);
            pthread_mutex_unlock(&ch->mu);
            return 1;
        }
    }
    pthread_mutex_unlock(&ch->mu);
    return 0;
}

// Non-blocking: returns 1 + writes to *out if a value is immediately available,
// 0 otherwise (empty, no sender ready, or closed-and-empty).
int64_t srt_chan_try_recv(void *ch_ptr, int64_t *out) {
    srt_chan_t *ch = (srt_chan_t *)ch_ptr;
    pthread_mutex_lock(&ch->mu);
    if (ch->capacity == 0) {
        if (ch->rv_has_value) {
            *out         = ch->rv_value;
            ch->rv_acked = 1;
            pthread_cond_signal(&ch->rv_sender_go);
            pthread_mutex_unlock(&ch->mu);
            return 1;
        }
    } else if (ch->count > 0) {
        *out      = ch->buf[ch->tail];
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
    pthread_cond_broadcast(&ch->rv_sender_go);
    pthread_mutex_unlock(&ch->mu);
}

int64_t srt_chan_is_closed(void *ch_ptr) {
    srt_chan_t *ch = (srt_chan_t *)ch_ptr;
    pthread_mutex_lock(&ch->mu);
    int64_t c = ch->closed;
    pthread_mutex_unlock(&ch->mu);
    return c;
}

// ── M-20: select { ch.recv() => body, default => body } ──────────────────────

// srt_select_try: non-blocking poll over n channels.
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

// srt_select: blocking select over n channels with exponential-backoff polling.
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

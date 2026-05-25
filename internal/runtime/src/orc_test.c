#include "soyuz.h"
#include <stdio.h>
#include <assert.h>

typedef struct Node {
    struct Node *next;
} Node;

static int nodes_freed = 0;

static void node_dtor(void *ptr) {
    Node *n = (Node *)ptr;
    soyuz_release(n->next);
    nodes_freed++;
}

static void node_trace(void *ptr, void (*visit)(void *child)) {
    Node *n = (Node *)ptr;
    if (n->next) visit(n->next);
}

static Node *node_new(void) {
    return (Node *)soyuz_alloc((int64_t)sizeof(Node), node_dtor, node_trace);
}

int main(void) {
    Node *a = node_new();
    Node *b = node_new();
    a->next = b;
    b->next = a;
    soyuz_retain(b);
    soyuz_retain(a);

    soyuz_release(a);
    soyuz_release(b);
    soyuz_orc_collect();

    assert(nodes_freed == 2);

    printf("orc cycle test ok\n");
    return 0;
}

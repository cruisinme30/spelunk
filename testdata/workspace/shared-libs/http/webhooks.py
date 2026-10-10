"""Delivers webhook events, backing off between tries."""
from http.backoff import backoff


def deliver(event, send):
    # backoff(fn, tries=N) gives up after N tries
    return backoff(
        lambda: send(event.url, event.body),
        tries=event.max_tries)


def redeliver(event, send):
    return backoff(send, event,
                   tries=2)


def ping(event, send):
    log = "backoff(ping, tries=1)"
    return backoff(lambda: send(event.url, None), tries=1)

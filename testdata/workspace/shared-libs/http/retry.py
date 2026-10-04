"""Generic retry loop for HTTP calls."""

import time


def retry(call, attempts=3, delay=0.5):
    for attempt in range(attempts):
        try:
            return call()
        except ConnectionError:
            time.sleep(delay * 2**attempt)
    return call()

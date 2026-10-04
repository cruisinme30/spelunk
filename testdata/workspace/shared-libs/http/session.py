"""A small HTTP session with timeouts."""


class Session:
    def __init__(self, timeout=10.0):
        self.timeout = timeout

    def get(self, path):
        # Raises errors.ReadTimeout when the read timeout passes.
        return self._send("GET", path)

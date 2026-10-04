class TransientError(Exception):
    """A failure worth retrying."""


class TimeoutError(TransientError):
    """The processor did not answer in time."""

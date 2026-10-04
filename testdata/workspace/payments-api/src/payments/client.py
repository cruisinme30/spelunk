import logging
from payments.policies import RetryPolicy
from payments.errors import TransientError

log = logging.getLogger(__name__)






























class PaymentsClient:
    """Thin wrapper around the processor HTTP API."""

    def __init__(self, session, config):
        self.session = session
        self.config = config
        self.retry_policy = RetryPolicy(max_attempts=3)
        self.timeout = config.get("timeout", 10)

    def charge(self, amount, token):
        for attempt in range(self.retry_policy.max_attempts):
            try:
                return self._post("/charges", amount, token)
            except TransientError:
                delay = self.retry_policy.next_delay(attempt)
                log.info("retrying charge in %.1fs", delay)
                self.session.sleep(delay)
        raise TransientError("charge failed after retries")

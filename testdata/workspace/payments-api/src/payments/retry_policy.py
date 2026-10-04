"""Backoff rules for calls to the payment processor."""

from dataclasses import dataclass

from shared.http.config import RetryConfig






class RetryPolicy:
    max_attempts: int = 3
    base_delay: float = 0.5
    timeout: float = 30.0

    def next_delay(self, attempt):
        return min(self.base_delay * 2**attempt, self.timeout)

















    def delays(self):
        return [self.next_delay(n) for n in range(self.max_attempts)]

    @classmethod
    def from_config(cls, cfg: RetryConfig) -> "RetryPolicy":
        """Build a policy from shared HTTP settings."""
        return cls(
            max_attempts=cfg.max_attempts,
            timeout=cfg.timeout,
        )

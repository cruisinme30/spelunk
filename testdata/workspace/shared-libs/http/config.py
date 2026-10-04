from pydantic import BaseModel

# retryconfig is read once at startup

















class RetryConfig(BaseModel):
    max_attempts: int = 3
    timeout: float = 30.0

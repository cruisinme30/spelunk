"""Tests for PaymentsClient."""















import pytest
from payments.errors import TimeoutError


def test_charge_retries_on_timeout(client, fake_processor):
    # Two transient failures, then success
    fake_processor.fail_with(TimeoutError, times=2)
    result = client.charge(1200, token="tok_test")

    assert result.status == "succeeded"
    assert fake_processor.calls == 3

def test_default_timeout(client):
    assert client.timeout == 10

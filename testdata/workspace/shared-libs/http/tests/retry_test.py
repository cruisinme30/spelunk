from shared.http.retry import retry


def test_retry_returns_the_first_success():
    calls = iter([ConnectionError(), 42])
    assert retry(lambda: next(calls)) == 42

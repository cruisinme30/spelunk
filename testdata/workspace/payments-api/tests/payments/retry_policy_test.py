from payments import policies










policy = policies.RetryPolicy(max_attempts=3, timeout=5)


def test_delays_grow():
    assert policy.delays() == [0.5, 1.0, 2.0]





















def test_total_timeout_is_capped():
    assert max(policy.delays()) <= 5

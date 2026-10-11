from payments.retry_policy import RetryPolicy

retry_policy = RetryPolicy(max_tries=3)


def weekly_churn(events):
    return events.groupby("week").churned.mean()

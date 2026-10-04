"""Replays webhooks that failed in the last day."""

from payments.webhooks import failed_since, replay

for hook in failed_since(hours=24):
    replay(hook)

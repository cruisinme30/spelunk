import pytest
from shared.http import Session, errors





session = Session(timeout=2.5)








def test_slow_server_raises(slow_server):
    session.base_url = slow_server
    with pytest.raises(errors.ReadTimeout):
        session.get("/slow")

import json

import pytest
from fastapi.testclient import TestClient

from document_processor.main import app


@pytest.fixture
def client(monkeypatch):
    monkeypatch.setenv("PROCESSOR_TOKEN", "test-only-token")
    with TestClient(app) as client:
        yield client


def test_private_endpoint_rejects_missing_and_wrong_tokens(client):
    assert client.get("/internal/v1/ready").status_code == 401
    assert (
        client.get("/internal/v1/ready", headers={"Authorization": "Bearer wrong"}).status_code
        == 401
    )


def test_contract_never_reports_stub_as_successful_extraction(client):
    metadata = {
        "request_id": "request-1",
        "document_version_id": "document-1",
        "sha256": "a" * 64,
        "profile_id": "demo",
        "profile_version": "1",
    }
    result = client.post(
        "/internal/v1/extract",
        headers={"Authorization": "Bearer test-only-token"},
        data={"metadata": json.dumps(metadata)},
        files={"file": ("example.docx", b"synthetic", "application/octet-stream")},
    )
    assert result.status_code == 501
    assert result.json()["code"] == "extraction_not_implemented"


def test_metadata_does_not_accept_arbitrary_download_urls(client):
    result = client.post(
        "/internal/v1/extract",
        headers={"Authorization": "Bearer test-only-token"},
        data={"metadata": json.dumps({"download_url": "http://example.invalid/private"})},
        files={"file": ("example.pdf", b"synthetic", "application/pdf")},
    )
    assert result.status_code == 422


def test_missing_secret_prevents_startup(monkeypatch):
    monkeypatch.delenv("PROCESSOR_TOKEN", raising=False)
    with pytest.raises(RuntimeError, match="PROCESSOR_TOKEN"):
        with TestClient(app):
            pass

import os
import secrets
from contextlib import asynccontextmanager
from typing import Annotated

from fastapi import Depends, FastAPI, File, Form, HTTPException, UploadFile
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer
from pydantic import ValidationError

from document_processor.contracts import ErrorResponse, ExtractionMetadata

security = HTTPBearer(auto_error=False)


@asynccontextmanager
async def lifespan(app: FastAPI):
    if not os.environ.get("PROCESSOR_TOKEN"):
        raise RuntimeError("PROCESSOR_TOKEN is required")
    yield


app = FastAPI(title="HackMax document processor", version="0.1.0", lifespan=lifespan)


def authorize(
    credentials: Annotated[HTTPAuthorizationCredentials | None, Depends(security)],
) -> None:
    token = os.environ.get("PROCESSOR_TOKEN", "")
    if not token:
        raise HTTPException(status_code=503, detail="Service is not configured")
    if credentials is None or not secrets.compare_digest(credentials.credentials, token):
        raise HTTPException(status_code=401, detail="Invalid service credentials")


@app.get("/healthz")
def health() -> dict[str, str]:
    return {"status": "ok", "service": "document-processor"}


@app.get("/internal/v1/ready", dependencies=[Depends(authorize)])
def ready() -> dict[str, object]:
    # Infrastructure readiness is intentionally separate from extraction capabilities.
    return {"status": "ready", "stage": "scaffold", "profiles": [], "ocr_enabled": False}


@app.post(
    "/internal/v1/extract",
    status_code=501,
    response_model=ErrorResponse,
    dependencies=[Depends(authorize)],
)
async def extract(
    metadata: Annotated[str, Form()],
    file: Annotated[UploadFile, File()],
) -> ErrorResponse:
    """Contract stub. No original is persisted and no successful extraction is simulated."""
    try:
        ExtractionMetadata.model_validate_json(metadata)
    except ValidationError:
        raise HTTPException(status_code=422, detail="Invalid extraction metadata") from None
    finally:
        await file.close()
    return ErrorResponse(
        code="extraction_not_implemented",
        message="Document extraction profiles and OCR are not implemented yet.",
    )

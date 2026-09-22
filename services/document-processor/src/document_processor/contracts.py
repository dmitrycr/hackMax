"""Versioned contract. Values are decimal strings; OCR scores are not probabilities."""

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class ExtractionMetadata(StrictModel):
    request_id: str = Field(min_length=1, max_length=128)
    document_version_id: str = Field(min_length=1, max_length=128)
    sha256: str = Field(pattern=r"^[a-f0-9]{64}$")
    profile_id: str = Field(min_length=1, max_length=128)
    profile_version: str = Field(min_length=1, max_length=32)
    allow_ocr: bool = False


class Source(StrictModel):
    kind: Literal["docx", "pdf", "image"]
    page: int | None = Field(default=None, ge=1)
    locator: str | None = None
    # Original visible page; x0,y0,x1,y1 in [0,1], origin top-left.
    bbox: tuple[float, float, float, float] | None = None


class ExtractedField(StrictModel):
    field_id: str
    raw_text: str | None
    normalized_value: str | None
    value_type: Literal["text", "decimal", "date"]
    extraction_method: Literal["docx", "pdf_text", "ocr"]
    engine_score: float | None = None
    review_required: bool
    reason_codes: list[str] = Field(default_factory=list)
    source: Source | None = None


class ExtractionResult(StrictModel):
    schema_version: Literal["1"] = "1"
    request_id: str
    document_version_id: str
    sha256: str
    profile_id: str
    profile_version: str
    processor_version: str
    status: Literal["complete", "partial", "unreadable"]
    fields: list[ExtractedField]
    issues: list[str]


class ErrorResponse(StrictModel):
    code: str
    message: str

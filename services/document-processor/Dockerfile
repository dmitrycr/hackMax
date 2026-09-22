FROM python:3.12-slim-bookworm
RUN pip install --no-cache-dir uv==0.11.6
RUN apt-get update && apt-get install -y --no-install-recommends tesseract-ocr tesseract-ocr-rus tesseract-ocr-eng && rm -rf /var/lib/apt/lists/*
WORKDIR /app
ENV UV_COMPILE_BYTECODE=1 UV_LINK_MODE=copy PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 PYTHONPATH=/app/src
COPY services/document-processor/pyproject.toml services/document-processor/uv.lock ./
RUN uv sync --frozen --no-dev --no-install-project
COPY services/document-processor/src ./src
RUN useradd --uid 10001 --no-create-home app
USER app
EXPOSE 8000
CMD ["/app/.venv/bin/uvicorn", "document_processor.main:app", "--host", "0.0.0.0", "--port", "8000", "--no-access-log"]

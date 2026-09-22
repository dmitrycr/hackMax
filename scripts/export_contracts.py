"""Export the processor OpenAPI and the planned extraction-result schema."""

import json
import sys
from pathlib import Path

root = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(root / "services/document-processor/src"))
from document_processor.contracts import ExtractionResult  # noqa: E402
from document_processor.main import app  # noqa: E402

outputs = {
    "processor.openapi.json": app.openapi(),
    "extraction-result.schema.json": ExtractionResult.model_json_schema(),
}
for name, value in outputs.items():
    (root / "contracts" / name).write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n")
    print(f"Exported contracts/{name}")

"""Create local development secrets once, never overwrite a user's .env."""

import secrets
from pathlib import Path

root = Path(__file__).resolve().parents[1]
target = root / ".env"
if target.exists():
    print(".env already exists; left unchanged")
else:
    text = (root / ".env.example").read_text()
    text = text.replace("local-hackmax-password", secrets.token_hex(20))
    text = text.replace("local-processor-token-change-me", secrets.token_hex(32))
    with target.open("x") as stream:
        stream.write(text)
    target.chmod(0o600)
    print("Created .env with local secrets; keep this file out of Git")

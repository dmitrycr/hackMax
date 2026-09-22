# Контракты

`processor.openapi.json` описывает реально доступный внутренний Python API.
`extraction-result.schema.json` описывает будущий результат извлечения; endpoint
пока возвращает HTTP 501 и не выдаёт результат по этой схеме.

Источник схем: `services/document-processor/src/document_processor/contracts.py`
и маршруты FastAPI. После изменения выполнять `make generate-contracts`.

Go передаёт файл байтами и JSON-метаданные, Python не получает произвольные URL,
путь файлового хранилища, доступ к PostgreSQL или токен MAX. Правила услуги живут в Go.


# Single stage: unlike pump-cv there is no GPU stack here, so the whole image
# is small enough that the split base/runtime build buys nothing.
FROM python:3.12-slim

WORKDIR /app

COPY pyproject.toml ./
COPY pump_voltra ./pump_voltra
COPY configs ./configs

RUN --mount=type=cache,target=/root/.cache/pip \
    pip install --no-cache-dir .

ENV PUMP_VOLTRA_CONFIG=/app/configs/default.yaml

# The trainer accepts a single BLE central, so this must run one replica with
# a Recreate strategy — two would fight over the connection.
EXPOSE 8080

ENTRYPOINT ["python", "-m", "pump_voltra.main"]

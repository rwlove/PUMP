# pump-cv runtime image.
#
# Targets a CUDA-capable Linux node. The runtime base image carries CUDA
# 12.x and cuDNN; PyTorch and its CUDA wheels are installed via pip on top.
#
# PyTorch is pinned to 2.5.1+cu121 to keep Pascal (sm_61, e.g. Tesla P40)
# in the wheel's arch list. PyTorch 2.6+ on PyPI ships the cu126 build,
# which dropped sm_60/sm_61. Using --extra-index-url alone is not enough:
# pip still sees PyPI and picks the higher version, so the +cu121 local
# version qualifier is required to force the cu121 wheel.
#
# Ubuntu 22.04 base — nvidia/cuda doesn't publish a 12.4.1 ubuntu24.04
# variant (we tried). 22.04 ships python3.10 system-wide, which is
# everything our code actually needs (we use 3.10+ features, none
# 3.12-specific).
FROM nvidia/cuda:12.4.1-cudnn-runtime-ubuntu22.04 AS runtime

ENV DEBIAN_FRONTEND=noninteractive \
    PYTHONUNBUFFERED=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PIP_NO_CACHE_DIR=1

RUN apt-get update && apt-get install -y --no-install-recommends \
        python3 \
        python3-venv \
        python3-pip \
        ffmpeg \
        libgl1 \
        libglib2.0-0 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Install Python deps first to keep layers cacheable.
COPY pyproject.toml ./
RUN pip install --upgrade pip \
    && pip install --extra-index-url https://download.pytorch.org/whl/cu121 \
        "torch==2.5.1+cu121" "torchvision==0.20.1+cu121" \
    && pip install .

COPY pump_cv ./pump_cv
COPY configs ./configs

ENV PUMP_CV_CONFIG=/app/configs/default.yaml

ENTRYPOINT ["python", "-m", "pump_cv.main"]

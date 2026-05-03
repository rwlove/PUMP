# pump-cv runtime image.
#
# Targets a CUDA-capable Linux node. The runtime base image carries CUDA
# 12.x and cuDNN; PyTorch and its CUDA wheels are installed via pip on top.
# Pascal-class GPUs (e.g. P40) are still supported by the current PyTorch
# CUDA 12 builds, just without modern tensor-core features.
#
# Ubuntu 24.04 base because it ships python3.12 natively in its default
# package repos; the older ubuntu22.04 nvidia/cuda image only has
# python3.10 without external PPAs.
FROM nvidia/cuda:12.4.1-cudnn-runtime-ubuntu24.04 AS runtime

ENV DEBIAN_FRONTEND=noninteractive \
    PYTHONUNBUFFERED=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_BREAK_SYSTEM_PACKAGES=1

RUN apt-get update && apt-get install -y --no-install-recommends \
        python3.12 \
        python3.12-venv \
        python3-pip \
        ffmpeg \
        libgl1 \
        libglib2.0-0 \
    && ln -sf /usr/bin/python3.12 /usr/local/bin/python \
    && ln -sf /usr/bin/python3.12 /usr/local/bin/python3 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Install Python deps first to keep layers cacheable.
COPY pyproject.toml ./
RUN pip install --upgrade pip \
    && pip install --extra-index-url https://download.pytorch.org/whl/cu121 \
        torch torchvision \
    && pip install .

COPY pump_cv ./pump_cv
COPY configs ./configs

ENV PUMP_CV_CONFIG=/app/configs/default.yaml

ENTRYPOINT ["python", "-m", "pump_cv.main"]

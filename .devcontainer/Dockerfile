# Use the official VS Code dev container base image (Ubuntu 22.04)
FROM mcr.microsoft.com/vscode/devcontainers/base:ubuntu-22.04

# Avoid interactive prompts by setting environment variables
ENV DEBIAN_FRONTEND=noninteractive

# Update and install essential packages
RUN apt-get update && apt-get install -y \
    curl \
    gnupg2 \
    build-essential \
    gzip \
    ca-certificates \
    apt-transport-https \
    lsb-release \
    && rm -rf /var/lib/apt/lists/*

# -------------------------
# Install Node.js v20
# -------------------------
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
    && apt-get update && apt-get install -y nodejs

# -------------------------
# Install Yarn (globally via npm)
# -------------------------
RUN npm install -g yarn

# -------------------------
# Install Go (v1.21)
# -------------------------
ENV GO_VERSION=1.21.0
ARG TARGETARCH
RUN if [ "$TARGETARCH" = "arm64" ]; then \
        curl -fsSL https://go.dev/dl/go${GO_VERSION}.linux-arm64.tar.gz -o go${GO_VERSION}.linux-${TARGETARCH}.tar.gz; \
    else \
        curl -fsSL https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz -o go${GO_VERSION}.linux-${TARGETARCH:-amd64}.tar.gz; \
    fi \
    && tar -C /usr/local -xzf go${GO_VERSION}.linux-${TARGETARCH:-amd64}.tar.gz \
    && rm go${GO_VERSION}.linux-${TARGETARCH:-amd64}.tar.gz
ENV PATH="/usr/local/go/bin:${PATH}"

# -------------------------
# (Optional) Install Docker CLI if needed for local image builds.
# -------------------------
RUN apt-get update && apt-get install -y docker.io

# -------------------------
# Set up the working directory
# -------------------------
WORKDIR /workspace

# Switch to non-root user provided by the dev container image
USER vscode

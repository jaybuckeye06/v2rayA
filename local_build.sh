#!/bin/bash

set -e  # Exit on error

# Detect host architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)  GOARCH="amd64" ;;
    aarch64) GOARCH="arm64" ;;
    armv7l)  GOARCH="arm" ;;
    *) 
        echo "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

# Define variables
VERSION="2.2.6.6"  # You can change this dynamically
DOCKER_IMAGE="jaybuckeye2006/v2raya"

echo "Using detected architecture: $GOARCH"

# Function to build the GUI
build_gui() {
    echo "Building GUI..."
    yarn --cwd gui --check-files
    yarn --cwd gui build
    echo "Compressing GUI files..."
    tar -zcvf web.tar.gz web/
}

# Function to build v2rayA binaries
build_binaries() {
    echo "Building v2rayA binaries..."
    export CGO_ENABLED=0
    export GOARCH="$GOARCH"

    rm -rf service/server/router/web/
    mkdir -p service/server/router/web && mv web/* service/server/router/web/
    mkdir -p v2raya_binaries
    cd service
    go build -tags "with_gvisor" -o ../v2raya_binaries/v2raya_linux-${GOARCH}_${VERSION} -ldflags="-X github.com/v2rayA/v2rayA/conf.Version=${VERSION} -s -w" -trimpath
    cd ..
}

# Function to build and push Docker image
build_docker_image() {
    echo "Building Docker image..."
    # Backup original docker_helper.sh
    cp install/docker/docker_helper.sh install/docker/docker_helper.sh.bak
    # Modify the version in the original file
    sed -i "s|Realv2rayAVersion|$VERSION|g" install/docker/docker_helper.sh
    sudo docker build -t "$DOCKER_IMAGE:$VERSION" -t "$DOCKER_IMAGE:latest" -f install/docker/Dockerfile.Action .
    # Restore original docker_helper.sh
    mv install/docker/docker_helper.sh.bak install/docker/docker_helper.sh
}

# Execution flow
build_gui
build_binaries
build_docker_image

echo "Build and Docker image creation completed!"
echo "Run 'docker run --rm -it $DOCKER_IMAGE:$VERSION' to test the image."

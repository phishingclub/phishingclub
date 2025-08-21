#!/bin/bash
set -e

# Get the current version from the VERSION file
VERSION=$(cat phishingclub/frontend/version/VERSION | tr -d '\n\r ')

# Check if version is valid
if [[ ! $VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "Error: Invalid version format. Expected semver format (e.g., 0.9.0)"
    exit 1
fi

# Get current git hash
GIT_HASH=$(git rev-parse --short HEAD)

# Create build directory
mkdir -p build

# Prompt for confirmation
echo "Ready to build and tag release v$VERSION ($GIT_HASH)"
read -p "Continue? (y/n): " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Operation cancelled"
    exit 1
fi

# Build frontend
echo "Building frontend..."
# remove any old builds
rm -rf phishingclub/frontend/frontend/build
mkdir -p phishingclub/frontend/frontend/build

sudo docker run --rm \
-v "$(pwd)":/app \
-w /app/phishingclub/frontend \
node:alpine \
sh -c "npm ci && npm run build-production"

# Get current user and group IDs
USER_ID=$(id -u)
GROUP_ID=$(id -g)

sudo chown -R $USER_ID:$GROUP_ID phishingclub/frontend/build
mv phishingclub/frontend/build ./phishingclub/frontend/frontend/

# Build the application
echo "Building application..."
sudo docker run --rm \
-v "$(pwd)":/app \
-w /app/phishingclub/frontend \
golang:alpine \
go build -trimpath \
-ldflags="-X github.com/phishingclub/phishingclub/version.hash=ph$GIT_HASH" \
-tags production -o ../build/phishingclub_${VERSION} main.go

echo "Build completed successfully: build/phishingclub_${VERSION}"


echo "Build completed successfully!"
echo "Created files:"
ls -lh build/
cd ..


echo "Release tagged as v$VERSION"

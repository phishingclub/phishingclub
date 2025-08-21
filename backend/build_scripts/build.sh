#!/bin/sh
echo "### Building frontend"
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
sudo mv phishingclub/frontend/build ./phishingclub/frontend/frontend/

echo "### Building backend"
HASH=$(git rev-parse --short HEAD)
echo "Building with hash: $HASH"


echo "building..."
sudo docker run --rm \
-v "$(pwd)":/app \
-w /app/phishingclub/frontend \
golang:alpine \
go build -trimpath \
-ldflags="-X github.com/phishingclub/phishingclub/version.hash=ph$HASH" \
-tags production -o ../build/phishingclub main.go

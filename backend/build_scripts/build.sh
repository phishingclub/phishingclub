#!/bin/sh
set -e

# build a single, self contained binary with the frontend embedded
# uses docker only, no host go or node toolchain is needed
# output is build/phishingclub at the repo root
#
# this mirrors .github/workflows/release.yml: same pinned images, same
# musl static cgo build. pinning the images is what keeps the frontend
# build reproducible, a floating node tag pulls a newer node that can
# run out of heap on the production bundle.
#
# defaults to linux amd64, override the arch with:
#   BIN_ARCH=arm64 ./backend/build_scripts/build.sh   (needs qemu locally)
# override the version string (defaults to git describe) with:
#   VERSION=1.2.3 ./backend/build_scripts/build.sh

# pinned images, keep in sync with .github/workflows/release.yml
NODE_IMAGE="node@sha256:968df39aedcea65eeb078fb336ed7191baf48f972b4479711397108be0966920" # node:22-alpine
GO_IMAGE_AMD64="golang@sha256:c4ea15b4a7912716eb362a022e2b12317762eca387423760bc59c0f9ae69423c" # golang:1.25.10-alpine linux/amd64
GO_IMAGE_ARM64="golang@sha256:5331adf7f8a0803631d9dc28843e288874789c14b97a3d0b54ed13e59f9e0589" # golang:1.25.10-alpine linux/arm64

# resolve the repo root from this script location so it works from any cwd
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/../.." && pwd)
cd "$REPO_ROOT"

USER_ID=$(id -u)
GROUP_ID=$(id -g)
HASH=$(git rev-parse --short HEAD)
VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}
BIN_ARCH=${BIN_ARCH:-amd64}

case "$BIN_ARCH" in
	amd64) GO_IMAGE="$GO_IMAGE_AMD64"; PLATFORM="" ;;
	arm64) GO_IMAGE="$GO_IMAGE_ARM64"; PLATFORM="--platform linux/arm64" ;;
	*) echo "unsupported BIN_ARCH=$BIN_ARCH (use amd64 or arm64)"; exit 1 ;;
esac

echo "### Building frontend"
# remove any old builds so nothing stale gets embedded
rm -rf frontend/build backend/frontend/build

# NODE_OPTIONS only raises the heap ceiling, it does not change the output
# bundle. the production build can peak above node's 2gb default on machines
# with many cores (more parallel transforms in flight), where the small CI
# runner stays under it.
sudo docker run --rm \
	-e NODE_OPTIONS=--max-old-space-size=4096 \
	-v "$REPO_ROOT/frontend":/app \
	-w /app \
	"$NODE_IMAGE" \
	sh -c "npm ci && npm run build-production"

# place the compiled frontend where the go binary embeds it
# (backend/frontend/frontend.go does //go:embed build/*)
mkdir -p backend/frontend/build
cp -r frontend/build/* backend/frontend/build/

echo "### Building binary for linux/$BIN_ARCH (version $VERSION, hash ph$HASH)"
mkdir -p build

# cgo build statically linked against musl, matches the release binary
sudo docker run --rm $PLATFORM \
	-v "$REPO_ROOT":/app \
	-w /app/backend \
	"$GO_IMAGE" \
	sh -c "apk add --no-cache gcc musl-dev && go build -trimpath \
		-ldflags='-X github.com/phishingclub/phishingclub/version.hash=ph$HASH -X github.com/phishingclub/phishingclub/version.version=$VERSION -linkmode=external -extldflags=-static' \
		-tags production -o /app/build/phishingclub main.go"

# the containers run as root, hand the build outputs back to the caller
sudo chown -R "$USER_ID:$GROUP_ID" build frontend/build frontend/node_modules backend/frontend/build

echo "### Done"
ls -lh "$REPO_ROOT/build/phishingclub"

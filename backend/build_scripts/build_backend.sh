
#!/bin/sh

HASH=$(git rev-parse --short HEAD)
echo "Building backend with hash: $HASH"

sudo docker run --rm \
-v "$(pwd)":/app \
-w /app/phishingclub/frontend \
golang \
go build -trimpath \
    -ldflags="-X github.com/phishingclub/phishingclub/version.hash=ph$HASH" \
    -tags production -o ../build/phishingclub main.go

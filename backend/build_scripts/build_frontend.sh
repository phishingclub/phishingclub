#!/bin/sh

echo "### Building frontend"
sudo docker run --rm \
	-v "$(pwd)":/app \
	-w /app/phishingclub/frontend \
	node:alpine \
	sh -c "npm ci && npm run build-production"

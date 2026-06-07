#!/bin/bash

# Exit on any error
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PHISHINGCLUB_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

echo "Generating licenses..."

# Create temp directory if it doesn't exist
mkdir -p /tmp/licenses

# Generate backend licenses
echo "Generating backend licenses..."
sudo docker compose exec -T backend bash -c "go install github.com/google/go-licenses@latest && \
    go-licenses report --ignore github.com/phishingclub/phishingclub --template ./utils/ossTemplate.tpl ./... > /tmp/backend-licenses.md 2> /dev/null"
sudo docker compose cp backend:/tmp/backend-licenses.md /tmp/licenses/

# Generate frontend licenses
echo "Generating frontend licenses..."
sudo docker compose exec -T frontend bash -c "npm run --silent license-report > /tmp/frontend-licenses.json 2>/dev/null"
sudo docker compose cp frontend:/tmp/frontend-licenses.json /tmp/licenses/

# Combine licenses
echo "Combining licenses..."
mkdir -p "$PHISHINGCLUB_DIR/frontend/static/"
cat /tmp/licenses/backend-licenses.md > "$PHISHINGCLUB_DIR/frontend/static/licenses.txt"
echo -e "\n\n" >> "$PHISHINGCLUB_DIR/frontend/static/licenses.txt"
cat /tmp/licenses/frontend-licenses.json >> "$PHISHINGCLUB_DIR/frontend/static/licenses.txt"
echo -e "\n\n" >> "$PHISHINGCLUB_DIR/frontend/static/licenses.txt"
cat "$PHISHINGCLUB_DIR/THIRD_PARTY_LICENSES.md" >> "$PHISHINGCLUB_DIR/frontend/static/licenses.txt"

# Cleanup
rm -rf /tmp/licenses

echo "License file generated at $PHISHINGCLUB_DIR/frontend/static/licenses.txt"

#!/bin/bash

# Exit on any error
set -e

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
# Ensure the static directory exists
mkdir -p frontend/static/
cat /tmp/licenses/backend-licenses.md > frontend/static/licenses.txt
echo -e "\n\n" >> frontend/static/licenses.txt
cat /tmp/licenses/frontend-licenses.json >> frontend/static/licenses.txt
echo -e "\n\n" >> frontend/static/licenses.txt
cat THIRD_PARTY_LICENSES.md >> frontend/static/licenses.txt

# Cleanup
rm -rf /tmp/licenses

echo "License file generated at frontend/static/licenses.txt"

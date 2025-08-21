#!/bin/bash

# Phishing Club Development Dependencies Installation Script
# For Ubuntu/Debian systems - Docker only

set -e

echo "🎣 Phishing Club - Installing Development Dependencies"
echo "===================================================="

# Check if running as root
if [[ $EUID -eq 0 ]]; then
   echo "❌ This script should not be run as root directly."
   echo "   Please run it as a regular user. It will prompt for sudo when needed."
   exit 1
fi

# Check if running on supported OS
if ! command -v apt-get &> /dev/null; then
    echo "❌ This script is designed for Ubuntu/Debian systems with apt-get."
    echo "   Please install Docker manually on your system."
    exit 1
fi

echo "📦 Updating package list..."
sudo apt-get update

echo "🐳 Installing Docker..."
if ! command -v docker &> /dev/null; then
    # Install Docker
    sudo apt-get install -y \
        ca-certificates \
        curl \
        gnupg \
        lsb-release

    # Add Docker's official GPG key
    sudo mkdir -p /etc/apt/keyrings
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg

    # Set up the repository
    echo \
        "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu \
        $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

    # Update package list with Docker repo
    sudo apt-get update

    # Install Docker Engine
    sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

    # Add user to docker group
    sudo usermod -aG docker $USER

    echo "✅ Docker installed successfully"
else
    echo "✅ Docker already installed"
fi

echo "🔧 Installing Docker Compose..."
if ! command -v docker-compose &> /dev/null; then
    # Install docker-compose (standalone)
    sudo curl -L "https://github.com/docker/compose/releases/latest/download/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
    sudo chmod +x /usr/local/bin/docker-compose
    echo "✅ Docker Compose installed successfully"
else
    echo "✅ Docker Compose already installed"
fi

echo "🛠️  Installing Git and Make..."
sudo apt-get install -y git make

echo "🔒 Setting up Docker permissions..."
# Start Docker service
sudo systemctl enable docker
sudo systemctl start docker

# Test Docker installation
if docker --version &> /dev/null; then
    echo "✅ Docker is working correctly"
else
    echo "⚠️  Docker may need a system restart to work properly"
fi

echo ""
echo "🎉 Installation Complete!"
echo ""
echo "📝 Next steps:"
echo "1. If this is your first Docker installation, you may need to:"
echo "   - Log out and log back in (or restart your system)"
echo "   - Or run: newgrp docker"
echo ""
echo "2. Verify installation:"
echo "   docker --version"
echo "   docker-compose --version"
echo ""
echo "3. Start the Phishing Club platform:"
echo "   make up"
echo ""
echo "4. Access the platform at:"
echo "   http://localhost:8003"
echo ""
echo "⚠️  If you encounter permission issues with Docker, try:"
echo "   sudo reboot"
echo "   # or"
echo "   newgrp docker"

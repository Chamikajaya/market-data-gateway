#!/usr/bin/env bash

# configures Git to use .githooks/ 
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "📎 Configuring Git hooks path..."

cd "$SCRIPT_DIR"
git config core.hooksPath .githooks

chmod +x .githooks/*

echo "✅ Git hooks installed successfully!"
echo ""
echo "Hooks configured:"
echo "  • pre-commit  — runs golangci-lint on all services"
echo "  • commit-msg  — enforces Conventional Commits format"
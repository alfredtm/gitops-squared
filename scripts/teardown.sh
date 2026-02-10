#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="gitops-squared"

echo "==> Deleting kind cluster..."
kind delete cluster --name "$CLUSTER_NAME" 2>/dev/null || true

echo "==> Teardown complete."

#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="gitops-squared"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

echo "==> Creating kind cluster..."
kind create cluster --name "$CLUSTER_NAME" --config "$ROOT_DIR/kind-config.yaml"

echo "==> Creating namespace..."
kubectl create namespace gitops-squared

echo "==> Deploying Zot registry..."
kubectl apply -f "$ROOT_DIR/deploy/zot/deployment.yaml"
echo "    Waiting for Zot to be ready..."
kubectl -n gitops-squared rollout status deployment/zot --timeout=120s

echo "==> Applying CRD..."
kubectl apply -f "$ROOT_DIR/deploy/crd/platformresource.yaml"

echo "==> Building API server image..."
docker build -t gitops-squared-api:latest "$ROOT_DIR"
kind load docker-image gitops-squared-api:latest --name "$CLUSTER_NAME"

echo "==> Deploying API server..."
kubectl apply -f "$ROOT_DIR/deploy/api/deployment.yaml"
echo "    Waiting for API to be ready..."
kubectl -n gitops-squared rollout status deployment/api --timeout=120s

FLUX_VERSION="v2.7.5"
echo "==> Installing Flux ${FLUX_VERSION}..."
kubectl apply -f "https://github.com/fluxcd/flux2/releases/download/${FLUX_VERSION}/install.yaml"

echo "    Waiting for Flux controllers..."
kubectl -n flux-system wait --for=condition=available --timeout=120s deployment/source-controller
kubectl -n flux-system wait --for=condition=available --timeout=120s deployment/kustomize-controller

echo "==> Pushing initial empty catalog via API..."
# Port-forward to the API and create a dummy resource to bootstrap the catalog,
# then delete it. This ensures an empty catalog artifact exists in Zot.
kubectl -n gitops-squared port-forward svc/api 8080:8080 &
PF_PID=$!
sleep 3

# Just hit healthz to trigger the startup catalog push (Restore pushes an empty catalog).
curl -sf http://localhost:8080/healthz > /dev/null
kill $PF_PID 2>/dev/null || true
wait $PF_PID 2>/dev/null || true

echo "==> Applying Flux OCIRepository + Kustomization..."
kubectl apply -f "$ROOT_DIR/deploy/flux/ocirepository.yaml"
kubectl apply -f "$ROOT_DIR/deploy/flux/kustomization.yaml"

echo ""
echo "==> Setup complete!"
echo ""
echo "    To access the API:"
echo "      kubectl -n gitops-squared port-forward svc/api 8080:8080"
echo ""
echo "    Then:"
echo "      curl http://localhost:8080/healthz"
echo ""
echo "    Flux status:"
echo "      kubectl -n flux-system get ocirepositories,kustomizations"

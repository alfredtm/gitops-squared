#!/usr/bin/env bash
set -euo pipefail

API="http://localhost:8080"

echo "=== GitOps Squared Demo ==="
echo ""
echo "Prerequisite: kubectl -n gitops-squared port-forward svc/api 8080:8080"
echo ""

# Check API is running.
if ! curl -sf "${API}/healthz" > /dev/null 2>&1; then
  echo "Error: API server is not reachable at ${API}"
  echo "Run: kubectl -n gitops-squared port-forward svc/api 8080:8080"
  exit 1
fi

echo "1. Create a VM resource..."
curl -s -X POST "${API}/api/v1/resources" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "web-server",
    "spec": {
      "type": "vm",
      "size": "medium",
      "region": "us-east-1",
      "replicas": 2
    }
  }' | jq .

echo ""
echo "2. Create a database resource..."
curl -s -X POST "${API}/api/v1/resources" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "app-db",
    "spec": {
      "type": "database",
      "size": "large",
      "region": "us-east-1"
    }
  }' | jq .

echo ""
echo "3. List all resources..."
curl -s "${API}/api/v1/resources" | jq .

echo ""
echo "4. Waiting for Flux to reconcile (~15s)..."
sleep 15

echo ""
echo "5. Check PlatformResources in the cluster..."
kubectl get platformresources -o wide

echo ""
echo "6. Update the VM (scale up)..."
curl -s -X POST "${API}/api/v1/resources" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "web-server",
    "spec": {
      "type": "vm",
      "size": "large",
      "region": "us-east-1",
      "replicas": 5
    }
  }' | jq .

echo ""
echo "7. Waiting for Flux to reconcile (~15s)..."
sleep 15

echo ""
echo "8. Verify update in cluster..."
kubectl get platformresource web-server -o yaml | head -20

echo ""
echo "9. Delete the VM resource..."
curl -s -X DELETE "${API}/api/v1/resources/web-server" | jq .

echo ""
echo "10. Waiting for Flux to prune (~15s)..."
sleep 15

echo ""
echo "11. Verify deletion — only app-db should remain..."
kubectl get platformresources -o wide

echo ""
echo "=== Demo complete ==="
echo ""
echo "Flux status:"
kubectl -n flux-system get ocirepositories,kustomizations

#!/usr/bin/env bash
set -euo pipefail

echo "Cleaning up demo resources..."
kubectl delete namespace iris-demo --ignore-not-found=true --timeout=60s
echo "Cleanup complete."

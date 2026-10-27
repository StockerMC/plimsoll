#!/bin/sh
set -eu
cd "$(dirname "$0")/.."

kubectl apply -f deploy/demo/rbac.yaml
kubectl apply -f deploy/demo/vpa.yaml
go run ./cmd/recommender --once

echo "demo:         $(kubectl get vpa demo -o jsonpath='{.status.recommendation}')"
echo "demo-default: $(kubectl get vpa demo-default -o jsonpath='{.status.recommendation}')"

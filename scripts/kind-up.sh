#!/bin/sh
set -eu
cd "$(dirname "$0")/.."

cluster=plimsoll

if ! kind get clusters 2>/dev/null | grep -qx "$cluster"; then
	kind create cluster --name "$cluster" --config deploy/kind/kind-config.yaml
fi
kubectl config use-context "kind-$cluster"

kubectl apply -f deploy/metrics-server/components.yaml
kubectl apply -f deploy/vpa/vpa-v1-crd-gen.yaml
kubectl apply -f deploy/vpa/vpa-rbac.yaml
kubectl apply -f deploy/vpa/recommender-deployment.yaml

kubectl -n kube-system rollout status deployment/metrics-server --timeout=3m
kubectl -n kube-system rollout status deployment/vpa-recommender --timeout=3m
kubectl -n kube-system get pods

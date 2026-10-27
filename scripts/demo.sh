#!/bin/sh
set -eu
cd "$(dirname "$0")/.."

kubectl apply -f deploy/demo/rbac.yaml
kubectl delete -f deploy/demo/memload.yaml --ignore-not-found
kubectl apply -f deploy/demo/memload.yaml
kubectl apply -f deploy/demo/vpa.yaml
kubectl rollout status deployment/demo --timeout=3m

go build -o bin/recommender ./cmd/recommender
bin/recommender --recommender-interval=1m --memory-window=1m --memory-aggregation-interval=2m --memory-histogram-decay-half-life=10m &
trap 'kill $!' EXIT

target='{.status.recommendation.containerRecommendations[?(@.containerName=="memload")].target.memory}'
for _ in $(seq 10); do
	sleep 60
	echo "$(date +%T) usage $(kubectl top pod -l app=demo --no-headers 2>/dev/null | awk '{print $3}') demo $(kubectl get vpa demo -o jsonpath="$target") demo-default $(kubectl get vpa demo-default -o jsonpath="$target")"
done
kubectl get vpa demo -o jsonpath='{.status.recommendation}'
echo

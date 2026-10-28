#!/bin/sh
# Usage: demo.sh [oom]
set -eu
cd "$(dirname "$0")/.."

manifest=deploy/demo/memload${1:+-$1}.yaml

kubectl apply -f deploy/demo/rbac.yaml
kubectl delete deployment demo --ignore-not-found --wait
kubectl wait --for=delete pod -l app=demo --timeout=2m
kubectl apply -f "$manifest"
kubectl apply -f deploy/demo/vpa.yaml -f deploy/demo/vpa-ensemble.yaml
kubectl rollout status deployment/demo --timeout=3m

go build -o bin/recommender ./cmd/recommender
bin/recommender --recommender-interval=1m --memory-window=1m --memory-aggregation-interval=2m --memory-histogram-decay-half-life=10m &
trap 'kill $!' EXIT

target='{.status.recommendation.containerRecommendations[?(@.containerName=="memload")].target.memory}'
restarts='{.items[0].status.containerStatuses[0].restartCount}'
for _ in $(seq 10); do
	sleep 60
	echo "$(date +%T) usage $(kubectl top pod -l app=demo --no-headers 2>/dev/null | awk '{print $3}') restarts $(kubectl get pod -l app=demo -o jsonpath="$restarts") demo $(kubectl get vpa demo -o jsonpath="$target") demo-ensemble $(kubectl get vpa demo-ensemble -o jsonpath="$target") demo-default $(kubectl get vpa demo-default -o jsonpath="$target")"
done
for vpa in demo demo-ensemble; do
	kubectl get vpa "$vpa" -o jsonpath='{.status.recommendation}'
	echo
done

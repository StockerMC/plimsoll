#!/bin/sh
# Usage: demo.sh [oom|ensemble]
# ensemble runs 15 s windows for 15 min, past the ensemble's 12-window cold
# start and its 12-window max, and restarts the workload at 9 min.
set -eu
cd "$(dirname "$0")/.."

mode=${1:-}
manifest=deploy/demo/memload.yaml
interval=1m samples=10 every=60 restart_at=0
case $mode in
oom) manifest=deploy/demo/memload-oom.yaml ;;
ensemble) interval=15s samples=30 every=30 restart_at=18 ;;
esac

kubectl apply -f deploy/demo/rbac.yaml
kubectl delete deployment demo --ignore-not-found --wait
kubectl wait --for=delete pod -l app=demo --timeout=2m
kubectl apply -f "$manifest"
kubectl apply -f deploy/demo/vpa.yaml -f deploy/demo/vpa-ensemble.yaml
kubectl rollout status deployment/demo --timeout=3m

go build -o bin/recommender ./cmd/recommender
bin/recommender --recommender-interval="$interval" --memory-window="$interval" --memory-aggregation-interval=2m --memory-histogram-decay-half-life=10m &
trap 'kill $!' EXIT

target='{.status.recommendation.containerRecommendations[?(@.containerName=="memload")].target.memory}'
restarts='{.items[0].status.containerStatuses[0].restartCount}'
for i in $(seq "$samples"); do
	sleep "$every"
	echo "$(date +%T) usage $(kubectl top pod -l app=demo --no-headers 2>/dev/null | awk '{print $3}') restarts $(kubectl get pod -l app=demo -o jsonpath="$restarts") demo $(kubectl get vpa demo -o jsonpath="$target") demo-ensemble $(kubectl get vpa demo-ensemble -o jsonpath="$target") demo-default $(kubectl get vpa demo-default -o jsonpath="$target")"
	if [ "$i" -eq "$restart_at" ]; then
		kubectl rollout restart deployment/demo
	fi
done
for vpa in demo demo-ensemble; do
	kubectl get vpa "$vpa" -o jsonpath='{.status.recommendation}'
	echo
done

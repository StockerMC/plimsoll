#!/bin/sh
set -eu
cd "$(dirname "$0")/.."

kubectl apply -f deploy/vpa/hamster.yaml

path='{.status.recommendation.containerRecommendations[0].target.memory}'
for _ in $(seq 60); do
	if [ -n "$(kubectl get vpa hamster-vpa -o jsonpath="$path")" ]; then
		kubectl describe vpa hamster-vpa
		exit 0
	fi
	sleep 10
done
echo "no recommendation after 10 minutes" >&2
exit 1

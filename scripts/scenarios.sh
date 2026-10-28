#!/bin/sh
# Usage: scenarios.sh [steady|burst|restart|missing-metrics ...]
set -eu
cd "$(dirname "$0")/.."

tick=15
out=results/summary/scenarios-$(date +%F)
raw=results/raw/scenarios-$(date +%F)
mkdir -p "$out" "$raw"

go build -o bin/recommender ./cmd/recommender
go build -o bin/scenario-summary ./cmd/scenario-summary

ours=
start_ours() {
	bin/recommender --vpa-object-namespace="$ns" --memory-window=1m >>"$raw/$name-ours.log" 2>&1 &
	ours=$!
}
stop_ours() {
	kill "$ours" 2>/dev/null || true
	wait "$ours" 2>/dev/null || true
}
trap stop_ours EXIT

sides="ours ensemble upstream"
target='{.status.recommendation.containerRecommendations[0].target.memory}'
restarts='{.items[0].status.containerStatuses[0].restartCount}'
reason='{.items[0].status.containerStatuses[0].lastState.terminated.reason}'

sample() {
	row="$1,$2"
	for side in $sides; do
		row="$row,$(kubectl -n "$ns" get vpa "$side" -o jsonpath="$target" 2>/dev/null || true)"
	done
	for side in $sides; do
		row="$row,$(kubectl -n "$ns" get pod -l app="$side" -o jsonpath="$restarts" 2>/dev/null || true)"
		row="$row,$(kubectl -n "$ns" get pod -l app="$side" -o jsonpath="$reason" 2>/dev/null || true)"
	done
	echo "$row" >>"$out/$name.csv"
}

no_event() { :; }

restart_event() {
	[ "$1" -eq 240 ] || return 0
	stop_ours
	start_ours
	kubectl -n kube-system delete pod -l app=vpa-recommender --wait=false >>"$raw/$name-events.log"
	event=restart
}

metrics_event() {
	case $1 in
	180)
		kubectl -n kube-system scale deployment/metrics-server --replicas=0 >>"$raw/$name-events.log"
		event=metrics-down
		;;
	360)
		kubectl -n kube-system scale deployment/metrics-server --replicas=1 >>"$raw/$name-events.log"
		event=metrics-up
		;;
	esac
}

# run name workload seconds event-function
run() {
	name=$1
	ns=scenario-$name
	echo "$(date +%T) $name: $3 s"
	kubectl delete namespace "$ns" --ignore-not-found --wait >/dev/null
	kubectl create namespace "$ns" >/dev/null
	kubectl -n "$ns" apply -f deploy/scenarios/memload.yaml -f "deploy/scenarios/$2.yaml" >/dev/null
	kubectl -n "$ns" rollout status deployment/ours deployment/ensemble deployment/upstream --timeout=3m >/dev/null
	header=elapsed_s,event
	for side in $sides; do header="$header,${side}_target"; done
	for side in $sides; do header="$header,${side}_restarts,${side}_reason"; done
	echo "$header" >"$out/$name.csv"
	start_ours
	begin=$(date +%s)
	elapsed=0
	while [ "$elapsed" -le "$3" ]; do
		event=
		$4 "$elapsed"
		sample "$elapsed" "$event"
		elapsed=$((elapsed + tick))
		wait_s=$((begin + elapsed - $(date +%s)))
		if [ "$wait_s" -gt 0 ]; then
			sleep "$wait_s"
		fi
	done
	stop_ours
	kubectl delete namespace "$ns" --wait=false >/dev/null
}

# Fresh upstream state: its in-memory histograms outlive deleted VPAs for up to an hour,
# and on start it reloads checkpoints left in any scenario namespace from an earlier run.
for ns in $(kubectl get namespaces -o name | grep '^namespace/scenario-' || true); do
	kubectl delete "$ns" --wait >/dev/null
done
kubectl -n kube-system rollout restart deployment/vpa-recommender >/dev/null
kubectl -n kube-system rollout status deployment/vpa-recommender --timeout=3m >/dev/null

[ $# -gt 0 ] || set -- steady burst restart missing-metrics
for s in "$@"; do
	case $s in
	steady) run steady steady 300 no_event ;;
	burst) run burst burst 600 no_event ;;
	restart) run restart steady 480 restart_event ;;
	missing-metrics)
		run missing-metrics steady 480 metrics_event
		kubectl -n kube-system rollout status deployment/metrics-server --timeout=3m >/dev/null
		;;
	*) echo "unknown scenario $s" >&2; exit 2 ;;
	esac
done

bin/scenario-summary "$out"

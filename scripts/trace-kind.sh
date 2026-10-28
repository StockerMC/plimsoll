#!/bin/sh
# Usage: trace-kind.sh [compression]
# Plays each curve in deploy/curves/ in its own namespace to ours, the
# ensemble and upstream, with every half-life and interval divided by the
# compression (default 60: a 300 s trace window lasts 5 s, two days 48 min).
# The ensemble runs ensemble-held from configs/default.json, its 288-window
# fall rescaled to 1-minute windows.
set -eu
cd "$(dirname "$0")/.."

compression=${1:-60}
step=$((300 / compression))
duration=$((576 * step))
day=$((86400 / compression))
hour_windows=$((3600 / compression / 60))
[ "$hour_windows" -ge 1 ] || hour_windows=1
fall_windows=$((288 * step / 60))
tick=15
out=results/summary/trace-kind-$(date +%F)
raw=results/raw/trace-kind-$(date +%F)
mkdir -p "$out" "$raw"

go build -o bin/recommender ./cmd/recommender
go build -o bin/scenario-summary ./cmd/scenario-summary

curves=$(ls deploy/curves/*.csv)
sides="ours ensemble upstream"
target='{.status.recommendation.containerRecommendations[0].target.memory}'
restarts='{.items[0].status.containerStatuses[0].restartCount}'
reason='{.items[0].status.containerStatuses[0].lastState.terminated.reason}'

pids=
stop() {
	for pid in $pids; do kill "$pid" 2>/dev/null || true; done
	wait 2>/dev/null || true
	kubectl -n kube-system patch deployment vpa-recommender --type=json \
		-p '[{"op":"remove","path":"/spec/template/spec/containers/0/args"}]' >/dev/null 2>&1 || true
}
trap stop EXIT

for ns in $(kubectl get namespaces -o name | grep '^namespace/trace-' || true); do
	kubectl delete "$ns" --wait >/dev/null
done
kubectl -n kube-system patch deployment vpa-recommender --type=json -p "[{\"op\":\"add\",\"path\":\"/spec/template/spec/containers/0/args\",\"value\":[\"--memory-aggregation-interval=${day}s\",\"--memory-histogram-decay-half-life=${day}s\"]}]" >/dev/null
kubectl -n kube-system rollout status deployment/vpa-recommender --timeout=3m >/dev/null

# Ours starts with the restarted upstream, before the pods exist, so every
# side watches the same pre-roll.
launched=$(date +%s)
for curve in $curves; do
	ns=trace-$(basename "$curve" .csv)
	bin/recommender --vpa-object-namespace="$ns" --memory-window=1m \
		--memory-aggregation-interval="${day}s" --memory-histogram-decay-half-life="${day}s" \
		--ensemble-time-scale="$compression" --ensemble-hour-windows="$hour_windows" \
		--ensemble-overrun-weight=100 --ensemble-limit-change-weight=0 \
		--ensemble-fall-fraction=0.1 --ensemble-fall-windows="$fall_windows" >>"$raw/$ns-ours.log" 2>&1 &
	pids="$pids $!"
done

start=$(($(date +%s) + 120))
echo "$step" >"$raw/step"
echo "$start" >"$raw/start"
for curve in $curves; do
	ns=trace-$(basename "$curve" .csv)
	kubectl create namespace "$ns" >/dev/null
	kubectl -n "$ns" create configmap curve --from-file=curve.py=deploy/curves/curve.py \
		--from-file=curve.csv="$curve" --from-file=step="$raw/step" --from-file=start="$raw/start" >/dev/null
	kubectl -n "$ns" apply -f deploy/curves/workloads.yaml >/dev/null
	header=elapsed_s,event
	for side in $sides; do header="$header,${side}_target"; done
	for side in $sides; do header="$header,${side}_restarts,${side}_reason"; done
	header="$header,curve_mib"
	for side in $sides; do header="$header,${side}_usage"; done
	echo "$header" >"$out/$ns.csv"
done
for curve in $curves; do
	kubectl -n "trace-$(basename "$curve" .csv)" rollout status deployment/ours deployment/ensemble deployment/upstream --timeout=3m >/dev/null
done

wait_s=$((start - $(date +%s)))
[ "$wait_s" -le 0 ] || sleep "$wait_s"
echo "$(date +%T) $compression x: $duration s per curve, $(echo "$curves" | wc -l | tr -d ' ') curves at once"

elapsed=0
while [ "$elapsed" -le "$duration" ]; do
	for curve in $curves; do
		ns=trace-$(basename "$curve" .csv)
		at=$(($(date +%s) - start))
		row=$at,
		for side in $sides; do
			row="$row,$(kubectl -n "$ns" get vpa "$side" -o jsonpath="$target" 2>/dev/null || true)"
		done
		for side in $sides; do
			row="$row,$(kubectl -n "$ns" get pod -l app="$side" -o jsonpath="$restarts" 2>/dev/null || true)"
			row="$row,$(kubectl -n "$ns" get pod -l app="$side" -o jsonpath="$reason" 2>/dev/null || true)"
		done
		row="$row,$(grep -v '^#' "$curve" | sed -n "$((at / step % 576 + 1))p")"
		top=$(kubectl -n "$ns" top pod --no-headers 2>/dev/null || true)
		for side in $sides; do
			row="$row,$(echo "$top" | awk -v p="$side-" 'index($1, p) == 1 { print $3 }')"
		done
		echo "$row" >>"$out/$ns.csv"
	done
	elapsed=$((elapsed + tick))
	wait_s=$((start + elapsed - $(date +%s)))
	[ "$wait_s" -le 0 ] || sleep "$wait_s"
done

for curve in $curves; do
	kubectl delete namespace "trace-$(basename "$curve" .csv)" --wait=false >/dev/null
done
bin/scenario-summary --trace "$out"
cat >>"$out/summary.md" <<EOF

Run at ${compression}x compression (one 300 s trace window per $step s, two days in $((duration / 60)) min) on the curves in deploy/curves/, all three namespaces at once. Every side starts watching before the pods exist and sees the same $((start - launched)) s pre-roll holding each curve's first row. The recommenders sample at 1 min, so they see a time-compressed, coarsely sampled version of each curve, not the signal the replay scored. ensemble is ensemble-held from configs/default.json (--ensemble-overrun-weight 100 --ensemble-limit-change-weight 0 --ensemble-fall-fraction 0.1 --ensemble-fall-windows $fall_windows, its 288 five-minute windows at this compression) with --ensemble-time-scale $compression --ensemble-hour-windows $hour_windows. The curves are scaled so their p95 is 256 MiB, just above the 250 MiB floor on every side, so where a curve sits near its p95 (trace-359: median 238, peak 342 MiB) the targets sit at or near the floor for much of the run and part of the slack is the floor's.
EOF

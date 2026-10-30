#!/bin/sh
# Usage: trace-kind.sh [compression]
# Plays each curve in deploy/curves/ in its own namespace to ours, the raw ensemble, ensemble-held
# and upstream, with every half-life and interval divided by the compression (default 60: a 300 s
# trace window lasts 5 s, two days 48 min). MODE (default Off) is the VPAs' updateMode; any other
# mode sets memory limit = request so usage above it is OOM-killed. RUN names the output
# directories (default today's date). Held parameters come from CONFIG (default configs/default.json).
set -eu
cd "$(dirname "$0")/.."

compression=${1:-60}
mode=${MODE:-Off}
run=${RUN:-$(date +%F)}
config=${CONFIG:-configs/default.json}
step=$((300 / compression))
duration=$((576 * step))
day=$((86400 / compression))
hour_windows=$((3600 / compression / 60))
[ "$hour_windows" -ge 1 ] || hour_windows=1
tick=15
out=results/summary/trace-kind-$run
raw=results/raw/trace-kind-$run
if [ -e "$out" ]; then
	echo "$out exists; pick another RUN" >&2
	exit 2
fi
mkdir -p "$out/terminations" "$raw"

held=$(scripts/ensemble-flags.sh "$config" ensemble-held "$step")
go build -o bin/recommender ./cmd/recommender
go build -o bin/scenario-summary ./cmd/scenario-summary

curves=$(ls deploy/curves/*.csv)
sides="ours ensemble held upstream"
target='{.status.recommendation.containerRecommendations[0].target.memory}'
restarts='{.items[0].status.containerStatuses[0].restartCount}'
reason='{.items[0].status.containerStatuses[0].lastState.terminated.reason}'
term='{.metadata.labels.app},{.metadata.uid},{.status.containerStatuses[0].name},{.status.containerStatuses[0].@.terminated.containerID},{.status.containerStatuses[0].@.terminated.reason},{.status.containerStatuses[0].@.terminated.finishedAt}{"\n"}'
term_path="$(printf %s "$term" | sed 's/@/lastState/g')$(printf %s "$term" | sed 's/@/state/g')"
common="--memory-window=1m --memory-aggregation-interval=${day}s --memory-histogram-decay-half-life=${day}s --ensemble-time-scale=$compression --ensemble-hour-windows=$hour_windows"
raw_flags="--recommender-name=plimsoll --ensemble-recommender-name=plimsoll-ensemble $common"
held_flags="--recommender-name=plimsoll-held-percentile-off --ensemble-recommender-name=plimsoll-ensemble-held $common $held"

upstream_args() {
	kubectl -n kube-system get deployment vpa-recommender -o jsonpath='{.spec.template.spec.containers[0].args}'
}
set_upstream_args() {
	if [ -n "$1" ]; then
		kubectl -n kube-system patch deployment vpa-recommender --type=json \
			-p "[{\"op\":\"add\",\"path\":\"/spec/template/spec/containers/0/args\",\"value\":$1}]" >/dev/null
	elif [ -n "$(upstream_args)" ]; then
		kubectl -n kube-system patch deployment vpa-recommender --type=json \
			-p '[{"op":"remove","path":"/spec/template/spec/containers/0/args"}]' >/dev/null
	fi
	kubectl -n kube-system rollout status deployment/vpa-recommender --timeout=3m >/dev/null
}

# The restart columns sample only the first pod; oom_kills comes from this watch, which sees every
# pod's terminations, and watch.csv records each reconnect, after which the count is a lower bound.
watch_terminations() {
	while :; do
		echo "start,$(date +%s)" >>"$out/terminations/$1-watch.csv"
		kubectl -n "$1" get pods --watch -o jsonpath="$term_path" 2>>"$raw/$1-watch.log" | grep --line-buffered -v '^[^,]*,[^,]*,[^,]*,,' || true
		echo "exit,$(date +%s)" >>"$out/terminations/$1-watch.csv"
		sleep 1
	done >>"$out/terminations/$1.csv"
}

# sed -i takes different arguments on BSD and GNU.
edit() {
	f=$1
	shift
	sed "$@" "$f" >"$f.tmp"
	mv "$f.tmp" "$f"
}

saved_args=$(upstream_args)
restored=
pids=
stop() {
	for pid in $pids; do kill "$pid" 2>/dev/null || true; done
	pkill -f -- "-n trace-[^ ]* get pods --watch" 2>/dev/null || true
	wait 2>/dev/null || true
	[ -n "$restored" ] || set_upstream_args "$saved_args" || true
}
trap stop EXIT

for ns in $(kubectl get namespaces -o name | grep '^namespace/trace-' || true); do
	kubectl delete "$ns" --wait >/dev/null
done
set_upstream_args "$(printf '%s' "${saved_args:-[]}" | jq -c --arg d "${day}s" '. + ["--memory-aggregation-interval=\($d)", "--memory-histogram-decay-half-life=\($d)"]')"

# Ours starts with the restarted upstream, before the pods exist, so every
# side watches the same pre-roll.
launched=$(date +%s)
for curve in $curves; do
	ns=trace-$(basename "$curve" .csv)
	# shellcheck disable=SC2086
	bin/recommender --vpa-object-namespace="$ns" $raw_flags >>"$raw/$ns-ours.log" 2>&1 &
	pids="$pids $!"
	# shellcheck disable=SC2086
	bin/recommender --vpa-object-namespace="$ns" $held_flags >>"$raw/$ns-held.log" 2>&1 &
	pids="$pids $!"
done

start=$(($(date +%s) + 120))
echo "$step" >"$raw/step"
echo "$start" >"$raw/start"
for curve in $curves; do
	ns=trace-$(basename "$curve" .csv)
	kubectl create namespace "$ns" >/dev/null
	echo "workload,uid,container,container_id,reason,finished_at" >"$out/terminations/$ns.csv"
	echo "event,time" >"$out/terminations/$ns-watch.csv"
	watch_terminations "$ns" &
	pids="$pids $!"
	kubectl -n "$ns" create configmap curve --from-file=curve.py=deploy/curves/curve.py \
		--from-file=curve.csv="$curve" --from-file=step="$raw/step" --from-file=start="$raw/start" >/dev/null
	for side in $sides; do
		case $side in
		ours) name=plimsoll ;;
		ensemble) name=plimsoll-ensemble ;;
		held) name=plimsoll-ensemble-held ;;
		upstream) name= ;;
		esac
		sed -e "s/NAME/$side/g" -e "s/RECOMMENDER/$name/" -e "s/MODE/$mode/" deploy/curves/workload.yaml >"$raw/$ns-$side.yaml"
		if [ -z "$name" ]; then
			edit "$raw/$ns-$side.yaml" '/recommenders:/{N;d;}'
		fi
		if [ "$mode" = Off ]; then
			edit "$raw/$ns-$side.yaml" -e '/limits:/{N;d;}' -e '/resizePolicy:/{N;N;d;}'
		fi
		kubectl -n "$ns" apply -f "$raw/$ns-$side.yaml" >/dev/null
	done
	header=elapsed_s,event
	for side in $sides; do header="$header,${side}_target"; done
	for side in $sides; do header="$header,${side}_restarts,${side}_reason"; done
	header="$header,curve_mib"
	for side in $sides; do header="$header,${side}_usage"; done
	echo "$header" >"$out/$ns.csv"
done
for curve in $curves; do
	kubectl -n "trace-$(basename "$curve" .csv)" rollout status deployment/ours deployment/ensemble deployment/held deployment/upstream --timeout=3m >/dev/null
done
{
	echo "mode=$mode"
	echo "compression=$compression"
	echo "step_s=$step"
	echo "tick_s=$tick"
	echo "ours_ensemble_flags=$raw_flags"
	echo "held_flags=$held_flags"
	go version -m bin/recommender | awk '$1 == "build" && $2 ~ /^vcs\.(revision|modified)=/ {print "binary_" $2}'
	echo "binary_sha256=$(shasum -a 256 bin/recommender | cut -d' ' -f1)"
	echo "config=$config"
	echo "config_sha256=$(shasum -a 256 "$config" | cut -d' ' -f1)"
	echo "driver_sha256=$(shasum -a 256 "$0" | cut -d' ' -f1)"
	echo "workload_sha256=$(shasum -a 256 deploy/curves/workload.yaml | cut -d' ' -f1)"
	for curve in $curves; do echo "curve_sha256_$(basename "$curve" .csv)=$(shasum -a 256 "$curve" | cut -d' ' -f1)"; done
	echo "upstream_args_saved=$saved_args"
	echo "upstream_args_run=$(upstream_args)"
	echo "updater_args=$(kubectl -n kube-system get deployment vpa-updater -o jsonpath='{.spec.template.spec.containers[0].args}' 2>/dev/null || true)"
	echo "kubernetes=$(kubectl version -o json | jq -r .serverVersion.gitVersion)"
	echo "start=$start"
} >"$out/run.txt"

wait_s=$((start - $(date +%s)))
[ "$wait_s" -le 0 ] || sleep "$wait_s"
echo "$(date +%T) $compression x, $mode: $duration s per curve, $(echo "$curves" | wc -l | tr -d ' ') curves at once"

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
set_upstream_args "$saved_args"
restored=1
echo "upstream_args_restored=$(upstream_args)" >>"$out/run.txt"
bin/scenario-summary --trace "$out"
limits="The pods have no memory limit."
if [ "$mode" != Off ]; then
	limits="Pods start at memory request = limit = 256 Mi and upstream's updater (args in run.txt) applies each VPA's target to both, at most once per updater interval; usage above the applied limit is OOM-killed, so it shows up as oom_kills rather than exceedance."
fi
cat >>"$out/summary.md" <<SUMMARY

Run at ${compression}x compression (one 300 s trace window per $step s, two days in $((duration / 60)) min) with updateMode $mode on the curves in deploy/curves/, all three namespaces at once. Every side starts watching before the pods exist and sees the same $((start - launched)) s pre-roll holding each curve's first row. The recommenders sample at 1 min, so they see a time-compressed, coarsely sampled version of each curve, not the signal the replay scored. ensemble is the raw ensemble at its built-in weights; held is ensemble-held from $config ($held, its five-minute fall windows rescaled to 1-minute windows at this compression); both run with --ensemble-time-scale $compression --ensemble-hour-windows $hour_windows. Flags, hashes and upstream's saved, run and restored args are in run.txt. $limits The curves are scaled so their p95 is 256 MiB, just above the 250 MiB floor on every side, so where a curve sits near its p95 (trace-359: median 238, peak 342 MiB) the targets sit at or near the floor for much of the run and part of the slack is the floor's.
SUMMARY

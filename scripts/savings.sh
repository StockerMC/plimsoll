#!/bin/sh
# Usage: savings.sh RUN [side ...]
# Runs each side's workloads with updateMode Off, then switches to MODE and keeps sampling.
set -eu
cd "$(dirname "$0")/.."

run=$1
shift
[ $# -gt 0 ] || set -- plimsoll ensemble ratio upstream
tick=15
off_s=${OFF_S:-1620}
on_s=${ON_S:-1200}
mode=${MODE:-InPlaceOrRecreate}
config=${CONFIG:-configs/default.json}
batches=${BATCHES:-"s384:384:1536Mi,s288:288:576Mi s512:512:1024Mi,s320:320:960Mi"}

raw=results/raw/savings-$run
mkdir -p "$raw"
recommender=${RECOMMENDER:-bin/recommender}
if [ -z "${RECOMMENDER:-}" ]; then
	go build -o bin/recommender ./cmd/recommender
fi

ratio=$(scripts/ensemble-flags.sh "$config" ensemble-ratio)

rec=
start_rec() {
	"$recommender" --vpa-object-namespace="$ns" --memory-window=1m "$@" >>"$dir/recommender.log" 2>&1 &
	rec=$!
}
watcher=
stop_bg() {
	for p in $rec $watcher; do
		kill "$p" 2>/dev/null || true
		pkill -P "$p" 2>/dev/null || true
		wait "$p" 2>/dev/null || true
	done
	# The watch loop's kubectl outlives its subshell otherwise.
	pkill -f -- "-n $ns get pods --watch" 2>/dev/null || true
	rec=
	watcher=
}
trap stop_bg EXIT

pods_path='{range .items[*]}{.metadata.labels.app},{.metadata.name},{.metadata.uid},{.metadata.deletionTimestamp},{.status.conditions[?(@.type=="Ready")].status},{.spec.containers[0].resources.requests.memory},{.spec.containers[0].resources.limits.memory},{.status.containerStatuses[0].resources.requests.memory},{.status.containerStatuses[0].resources.limits.memory},{.status.conditions[?(@.type=="PodResizeInProgress")].status},{.status.conditions[?(@.type=="PodResizePending")].status},{.status.containerStatuses[0].restartCount},{.status.containerStatuses[0].name}{"\n"}{end}'
deploy_path='{range .items[*]}{.metadata.name},{.spec.replicas},{.status.readyReplicas}{"\n"}{end}'
vpa_path='{range .items[*]}{.metadata.name},{.status.recommendation.containerRecommendations[0].target.memory},{.spec.updatePolicy.updateMode}{"\n"}{end}'
term='{.metadata.labels.app},{.metadata.uid},{.status.containerStatuses[0].name},{.status.containerStatuses[0].@.terminated.containerID},{.status.containerStatuses[0].@.terminated.reason},{.status.containerStatuses[0].@.terminated.exitCode},{.status.containerStatuses[0].@.terminated.finishedAt}{"\n"}'
term_path="$(printf %s "$term" | sed 's/@/lastState/g')$(printf %s "$term" | sed 's/@/state/g')"

prefix() { sed "s/^/$1,/"; }

sample() {
	now=$(date +%s)
	kubectl -n "$ns" get pods -o jsonpath="$pods_path" | prefix "$now" >>"$dir/pods.csv"
	kubectl -n "$ns" get deploy -o jsonpath="$deploy_path" | prefix "$now" >>"$dir/deployments.csv"
	kubectl -n "$ns" get vpa -o jsonpath="$vpa_path" | prefix "$now" >>"$dir/vpas.csv"
	kubectl -n "$ns" top pods --containers --no-headers 2>/dev/null | awk -v t="$now" '{print t "," $1 "," $2 "," $4}' >>"$dir/usage.csv" || true
}

# Pods deleted between samples still show their terminations here; kubectl re-lists on reconnect, and
# watch.csv records each outage so a pod deleted during one makes the kill count a lower bound.
watch_terminations() {
	while :; do
		echo "start,$(date +%s)" >>"$dir/watch.csv"
		kubectl -n "$ns" get pods --watch -o jsonpath="$term_path" 2>>"$dir/watch.log" | grep --line-buffered -v '^[^,]*,[^,]*,[^,]*,,' || true
		echo "exit,$(date +%s)" >>"$dir/watch.csv"
		sleep 1
	done >>"$dir/terminations.csv"
}

sleep_until() {
	wait_s=$(($1 - $(date +%s)))
	if [ "$wait_s" -gt 0 ]; then
		sleep "$wait_s"
	fi
}

side_run() {
	side=$1 batch=$2 index=$3
	ns=savings-$run-$side-$index
	dir=$raw/$side-$index
	mkdir -p "$dir"
	case $side in
	plimsoll) name=plimsoll flags="--recommender-name=plimsoll --ensemble-recommender-name=" ;;
	ensemble) name=plimsoll-ensemble flags="--recommender-name=plimsoll --ensemble-recommender-name=plimsoll-ensemble" ;;
	ratio) name=plimsoll-ensemble-ratio flags="--recommender-name=plimsoll-ratio-percentile-off --ensemble-recommender-name=plimsoll-ensemble-ratio $ratio" ;;
	upstream) name= flags= ;;
	*) echo "unknown side $side" >&2; exit 2 ;;
	esac
	echo "$(date +%T) $ns"
	kubectl delete namespace "$ns" --ignore-not-found --wait >/dev/null
	kubectl create namespace "$ns" >/dev/null
	: >"$dir/manifest.yaml"
	for shape in $(echo "$batch" | tr , ' '); do
		IFS=: read -r w mib req <<SHAPE
$shape
SHAPE
		sed -e "s/NAME/$w/g" -e "s/\"MIB\"/\"$mib\"/" -e "s/REQUEST/$req/g" -e "s/RECOMMENDER/$name/" deploy/savings/workload.yaml >"$dir/$w.yaml"
		if [ -z "$name" ]; then
			sed '/recommenders:/{N;d;}' "$dir/$w.yaml" >"$dir/$w.yaml.tmp"
			mv "$dir/$w.yaml.tmp" "$dir/$w.yaml"
		fi
		cat "$dir/$w.yaml" >>"$dir/manifest.yaml"
		echo --- >>"$dir/manifest.yaml"
		rm "$dir/$w.yaml"
	done
	kubectl -n "$ns" apply -f deploy/savings/memload.yaml -f "$dir/manifest.yaml" >/dev/null
	kubectl -n "$ns" wait --for=condition=Available deploy --all --timeout=5m >/dev/null
	echo "time,workload,pod,uid,deleting,ready,spec_request,spec_limit,applied_request,applied_limit,resize_in_progress,resize_pending,restarts,container" >"$dir/pods.csv"
	echo "time,workload,desired,ready" >"$dir/deployments.csv"
	echo "time,workload,target,mode" >"$dir/vpas.csv"
	echo "workload,uid,container,container_id,reason,exit_code,finished_at" >"$dir/terminations.csv"
	echo "time,pod,container,memory" >"$dir/usage.csv"
	echo "event,time" >"$dir/watch.csv"
	watch_terminations &
	watcher=$!
	if [ -n "$name" ]; then
		# shellcheck disable=SC2086
		start_rec $flags
	fi
	begin=$(date +%s)
	{
		echo "side=$side"
		echo "namespace=$ns"
		echo "batch=$batch"
		echo "mode=$mode"
		echo "recommender_flags=$flags --vpa-object-namespace=$ns --memory-window=1m"
		go version -m "$recommender" | awk '$1 == "build" && $2 ~ /^vcs\.(revision|modified)=/ {print "binary_" $2}'
		echo "binary_sha256=$(shasum -a 256 "$recommender" | cut -d' ' -f1)"
		echo "config=$config"
		echo "config_sha256=$(shasum -a 256 "$config" | cut -d' ' -f1)"
		echo "off_s=$off_s"
		echo "on_s=$on_s"
		echo "upstream_recommender_pod=$(kubectl -n kube-system get pod -l app=vpa-recommender -o jsonpath='{.items[*].metadata.uid}')"
		echo "updater_args=$(kubectl -n kube-system get deploy vpa-updater -o jsonpath='{.spec.template.spec.containers[0].args}')"
		echo "begin=$begin"
	} >"$dir/run.txt"
	next=$begin
	while [ "$next" -lt $((begin + off_s)) ]; do
		sample
		next=$((next + tick))
		sleep_until "$next"
	done
	sample
	for w in $(kubectl -n "$ns" get vpa -o name); do
		kubectl -n "$ns" patch "$w" --type=merge -p "{\"spec\":{\"updatePolicy\":{\"updateMode\":\"$mode\"}}}" >/dev/null
	done
	switch=$(date +%s)
	echo "switch=$switch" >>"$dir/run.txt"
	next=$switch
	while [ "$next" -lt $((switch + on_s)) ]; do
		next=$((next + tick))
		sleep_until "$next"
		sample
	done
	{
		echo "end=$(date +%s)"
		echo "upstream_recommender_pod_end=$(kubectl -n kube-system get pod -l app=vpa-recommender -o jsonpath='{.items[*].metadata.uid}')"
	} >>"$dir/run.txt"
	kubectl -n "$ns" get pods -o yaml >"$dir/pods-end.yaml"
	kubectl -n "$ns" get events -o wide >"$dir/events.txt" 2>&1 || true
	kubectl -n kube-system logs deployment/vpa-updater --since=$((off_s + on_s + 120))s 2>&1 | grep "$ns" >"$dir/updater.log" || true
	stop_bg
	kubectl delete namespace "$ns" --wait >/dev/null
}

index=0
for batch in $batches; do
	index=$((index + 1))
	for side in "$@"; do
		side_run "$side" "$batch" "$index"
	done
done

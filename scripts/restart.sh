#!/bin/sh
# Usage: restart.sh RUN
# Steady 384 MiB workloads with 15 s windows; every recommender is restarted
# at 1260 s, once the held ensemble's 24-window fall has had time to
# release, and sampled for 660 s more. Sides: percentile, plain ensemble,
# held ensemble, ensemble with checkpoints off, upstream.
set -eu
cd "$(dirname "$0")/.."

run=${1:?usage: restart.sh RUN}
tick=15 restart_at=1260 duration=1920
ns=restart-$run
out=results/summary/restart-$run
raw=results/raw/restart-$run
if [ -e "$out" ] || [ -e "$raw" ]; then
	echo "$out or $raw exists; pick a new RUN" >&2
	exit 1
fi
mkdir -p "$out" "$raw"

go build -o bin/recommender ./cmd/recommender

sides="pct ens held nockpt upstream"
flags="--vpa-object-namespace=$ns --memory-window=15s --recommender-interval=15s"
cat >"$out/manifest" <<EOF
commit $(git rev-parse HEAD)$(git diff --quiet HEAD -- cmd internal go.mod go.sum || echo ' dirty')
recommender $(shasum -a 256 bin/recommender | cut -d' ' -f1)
flags $flags
held --ensemble-fall-windows=24; nockpt --checkpoints=false
tick ${tick}s restart ${restart_at}s duration ${duration}s
started $(date -u +%Y-%m-%dT%H:%M:%SZ)
EOF
pids=
start() {
	bin/recommender $flags --recommender-name=ckpt-pct --ensemble-recommender-name=ckpt-ens >>"$raw/a.log" 2>&1 &
	pids="$!"
	bin/recommender $flags --recommender-name=ckpt-b --ensemble-recommender-name=ckpt-held --ensemble-fall-windows=24 >>"$raw/held.log" 2>&1 &
	pids="$pids $!"
	bin/recommender $flags --recommender-name=ckpt-c --ensemble-recommender-name=ckpt-nockpt --checkpoints=false >>"$raw/nockpt.log" 2>&1 &
	pids="$pids $!"
}
stop() {
	for p in $pids; do kill "$p" 2>/dev/null || true; done
	for p in $pids; do wait "$p" 2>/dev/null || true; done
	pids=
}
trap stop EXIT

kubectl delete namespace "$ns" --ignore-not-found --wait >/dev/null
kubectl create namespace "$ns" >/dev/null
kubectl -n "$ns" apply -f deploy/scenarios/memload.yaml >/dev/null
for side in $sides; do
	recommenders="recommenders: [{name: ckpt-$side}]"
	[ "$side" = upstream ] && recommenders=
	kubectl -n "$ns" apply -f - >/dev/null <<EOF
apiVersion: apps/v1
kind: Deployment
metadata: {name: $side}
spec:
  replicas: 1
  selector: {matchLabels: {app: $side}}
  template:
    metadata: {labels: {app: $side}}
    spec:
      containers:
        - name: memload
          image: python:3.13-alpine
          command: ["python3", "-u", "/memload/steady.py"]
          volumeMounts: [{name: memload, mountPath: /memload}]
          resources: {requests: {memory: 128Mi}}
      volumes: [{name: memload, configMap: {name: memload}}]
---
apiVersion: autoscaling.k8s.io/v1
kind: VerticalPodAutoscaler
metadata: {name: $side}
spec:
  targetRef: {apiVersion: apps/v1, kind: Deployment, name: $side}
  $recommenders
  updatePolicy: {updateMode: "Off"}
  resourcePolicy: {containerPolicies: [{containerName: "*", controlledResources: [memory]}]}
EOF
done
for side in $sides; do
	kubectl -n "$ns" rollout status "deployment/$side" --timeout=3m >/dev/null
done

target='{.status.recommendation.containerRecommendations[0].target.memory}'
header=elapsed_s,event
for side in $sides; do header="$header,$side"; done
echo "$header" >"$out/targets.csv"

start
begin=$(date +%s)
elapsed=0
while [ "$elapsed" -le "$duration" ]; do
	event=
	if [ "$elapsed" -eq "$restart_at" ]; then
		kubectl -n "$ns" get verticalpodautoscalercheckpoints -o yaml >"$out/checkpoints-before.yaml"
		stop
		kubectl -n kube-system delete pod -l app=vpa-recommender --wait=false >/dev/null
		start
		event=restart
	fi
	row="$elapsed,$event"
	for side in $sides; do
		row="$row,$(kubectl -n "$ns" get vpa "$side" -o jsonpath="$target" 2>/dev/null || true)"
	done
	echo "$row" >>"$out/targets.csv"
	elapsed=$((elapsed + tick))
	wait_s=$((begin + elapsed - $(date +%s)))
	[ "$wait_s" -le 0 ] || sleep "$wait_s"
done
kubectl -n "$ns" get verticalpodautoscalercheckpoints -o yaml >"$out/checkpoints-after.yaml"
stop
kubectl delete namespace "$ns" --wait=false >/dev/null

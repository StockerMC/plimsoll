#!/bin/sh
set -eu
cd "$(dirname "$0")/.."

cluster=plimsoll

if ! kind get clusters 2>/dev/null | grep -qx "$cluster"; then
	kind create cluster --name "$cluster" --config deploy/kind/kind-config.yaml
fi
kubectl config use-context "kind-$cluster"

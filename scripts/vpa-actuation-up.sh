#!/bin/sh
# Deploys upstream's updater and admission controller next to the recommender from kind-up.sh.
set -eu
cd "$(dirname "$0")/.."

if ! kubectl -n kube-system get secret vpa-tls-certs >/dev/null 2>&1; then
	# Same certs as upstream pkg/admission-controller/gencerts.sh.
	dir=$(mktemp -d)
	trap 'rm -rf "$dir"' EXIT
	cat >"$dir/server.conf" <<CONF
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[ v3_req ]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth, serverAuth
subjectAltName = DNS:vpa-webhook.kube-system.svc
CONF
	openssl genrsa -out "$dir/caKey.pem" 2048 2>/dev/null
	openssl req -x509 -new -nodes -key "$dir/caKey.pem" -days 100000 -out "$dir/caCert.pem" -subj "/CN=vpa_webhook_ca" -addext "subjectAltName = DNS:vpa_webhook_ca"
	openssl genrsa -out "$dir/serverKey.pem" 2048 2>/dev/null
	openssl req -new -key "$dir/serverKey.pem" -out "$dir/server.csr" -subj "/CN=vpa-webhook.kube-system.svc" -config "$dir/server.conf"
	openssl x509 -req -in "$dir/server.csr" -CA "$dir/caCert.pem" -CAkey "$dir/caKey.pem" -CAcreateserial -out "$dir/serverCert.pem" -days 100000 -extensions v3_req -extfile "$dir/server.conf"
	kubectl -n kube-system create secret generic vpa-tls-certs \
		--from-file="$dir/caKey.pem" --from-file="$dir/caCert.pem" \
		--from-file="$dir/serverKey.pem" --from-file="$dir/serverCert.pem"
fi

kubectl apply -f deploy/vpa/admission-controller-deployment.yaml -f deploy/vpa/updater-deployment.yaml
kubectl -n kube-system rollout status deployment/vpa-admission-controller --timeout=3m
kubectl -n kube-system rollout status deployment/vpa-updater --timeout=3m
until kubectl get mutatingwebhookconfiguration vpa-webhook-config >/dev/null 2>&1; do
	sleep 2
done

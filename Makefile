.PHONY: test vet kind-up kind-down smoke

test: vet
	go test ./...

vet:
	go vet ./...

kind-up:
	scripts/kind-up.sh

kind-down:
	scripts/kind-down.sh

smoke:
	scripts/smoke.sh

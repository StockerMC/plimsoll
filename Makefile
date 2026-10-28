.PHONY: test vet kind-up kind-down smoke demo scenarios replay trace-kind

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

demo:
	scripts/demo.sh $(DEMO)

scenarios:
	scripts/scenarios.sh $(SCENARIOS)

replay:
	scripts/replay-all.sh $(STEP)

trace-kind:
	scripts/trace-kind.sh $(COMPRESSION)

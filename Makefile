.PHONY: test vet kind-up kind-down smoke demo scenarios savings replay

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

savings:
	scripts/vpa-actuation-up.sh
	scripts/savings.sh $(SAVINGS)
	go run ./cmd/savings-summary results/raw/savings-$(SAVINGS) results/summary/savings-$(SAVINGS)

replay:
	scripts/replay-all.sh $(STEP)

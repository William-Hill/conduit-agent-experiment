.PHONY: build run-task index report test lint clean triage

BINARY := conduit-experiment

build:
	go build -o bin/$(BINARY) ./cmd/experiment

index:
	go run ./cmd/experiment index

run-task:
	@test -n "$(TASK)" || (echo "Usage: make run-task TASK=data/tasks/task-001.json" && exit 1)
	go run ./cmd/experiment run --task $(TASK)

report:
	@test -n "$(RUN)" || (echo "Usage: make report RUN=run-001" && exit 1)
	go run ./cmd/experiment report --run-id $(RUN)

test:
	go test ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/

triage:
	go run ./cmd/triage console

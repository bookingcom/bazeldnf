all: gazelle buildifier

deps-update:
	bazelisk run //:gazelle

gazelle:
	bazelisk run //:gazelle

test: gazelle
	bazelisk test //pkg/...

buildifier:
	bazelisk run //:buildifier

gofmt:
	gofmt -w pkg/.. cmd/..

fmt: gofmt buildifier

.PHONY: gazelle test deps-update buildifier gofmt fmt

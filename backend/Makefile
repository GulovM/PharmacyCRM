.PHONY: test vet fmt

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

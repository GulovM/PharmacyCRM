.PHONY: architecture-check backend-test test

architecture-check:
	./scripts/check-architecture.sh

backend-test:
	$(MAKE) -C backend test

test: architecture-check backend-test

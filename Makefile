.PHONY: architecture-check backend-test format-check frontend-test test

ifeq ($(OS),Windows_NT)
ARCHITECTURE_CHECK = powershell -NoProfile -ExecutionPolicy Bypass -File scripts/check-architecture.ps1
FORMAT_CHECK = powershell -NoProfile -Command "$$unformatted = gofmt -l backend; if ($$unformatted) { Write-Error $$unformatted; exit 1 }"
else
ARCHITECTURE_CHECK = ./scripts/check-architecture.sh
FORMAT_CHECK = cd backend && test -z "$$(gofmt -l .)"
endif

architecture-check:
	$(ARCHITECTURE_CHECK)

format-check:
	$(FORMAT_CHECK)

backend-test:
	$(MAKE) -C backend test

frontend-test:
	pnpm --dir frontend install --frozen-lockfile
	pnpm --dir frontend lint
	pnpm --dir frontend typecheck
	pnpm --dir frontend test

test: architecture-check backend-test frontend-test

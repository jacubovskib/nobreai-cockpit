.PHONY: web build dev test

web:
	pnpm -C web install && pnpm -C web build

build: web
	go build -o nobred ./cmd/nobred

# Desenvolvimento: nobred em :7420 e Vite (com proxy de /api) em :5173.
dev:
	@echo "Rode 'go run ./cmd/nobred' em um terminal e 'pnpm -C web dev' em outro."

test:
	go vet ./... && go test ./... && pnpm -C web test --run

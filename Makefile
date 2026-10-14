.PHONY: run seed demo test vuln build deploy

run:
	@set -a; [ -f .env ] && . ./.env; set +a; \
	DB_PATH=$${DB_PATH:-shengen.db} \
	PORT=$${PORT:-8080} \
	DEV_INSECURE_COOKIES=$${DEV_INSECURE_COOKIES:-true} \
	go run ./cmd/server

seed:
	@set -a; [ -f .env ] && . ./.env; set +a; \
	DB_PATH=$${DB_PATH:-shengen.db} \
	go run ./cmd/seed $(ARGS)

demo:
	@set -a; [ -f .env ] && . ./.env; set +a; \
	DB_PATH=$${DB_PATH:-shengen.db} \
	go run ./cmd/demo $(ARGS)

test:
	go test ./... -v

# Proverava poznate ranjivosti u Go-u i modulima do kojih kod stvarno dolazi.
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

build:
	go build -o bin/shengen ./cmd/server

deploy: build
	@echo "TODO (Faza 6): scp bin/shengen + migrations na VPS, restart systemd servisa"

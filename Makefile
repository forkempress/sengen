.PHONY: run proba mailtest seed demo test vuln build deploy

run:
	@set -a; [ -f .env ] && . ./.env; set +a; \
	DB_PATH=$${DB_PATH:-shengen.db} \
	PORT=$${PORT:-8080} \
	DEV_INSECURE_COOKIES=$${DEV_INSECURE_COOKIES:-true} \
	go run ./cmd/server

# Proba izmena bez dev okruženja: aplikacija nad KOPIJOM produkcijske baze
# (sqlite3 .backup radi i dok produkcija radi), na http://localhost:8095.
# Bez Telegram bota i superadmina, pa ne šalje ništa vozačima ni operatorima;
# mejlovi se samo ispisuju u log. PROBA_SMTP=1 zadržava SMTP iz .env, da se
# proba pravo slanje (na sopstvenu adresu). Kopija je u proba/ i ostaje do
# sledećeg pokretanja.
proba:
	@set -a; [ -f .env ] && . ./.env; set +a; \
	mkdir -p proba && \
	sqlite3 "$${DB_PATH:-shengen.db}" ".backup proba/proba.db" && \
	echo "Kopija baze: proba/proba.db — otvorite http://localhost:8095/login" && \
	env -u TELEGRAM_BOT_TOKEN -u TELEGRAM_WEBHOOK_SECRET -u TELEGRAM_BOT_USERNAME -u ADMIN_TELEGRAM_IDS \
	    -u BACKUP_HEARTBEAT_FILE -u TURNSTILE_SITE_KEY -u TURNSTILE_SECRET_KEY $(if $(PROBA_SMTP),,-u SMTP_HOST) \
	    DB_PATH=proba/proba.db PORT=8095 DEV_INSECURE_COOKIES=true PUBLIC_BASE_URL= TRUSTED_PROXY=none \
	    go run ./cmd/server

# Proverava SMTP iz .env; ARGS="-to adresa" šalje i jedan probni mejl.
mailtest:
	@set -a; [ -f .env ] && . ./.env; set +a; \
	go run ./cmd/mailtest $(ARGS)

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

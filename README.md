# Evidencija 90/180

Praćenje dana koje vozači provedu u Šengen zoni, zbog pravila „najviše 90 dana
u bilo kom periodu od 180 dana". Od 2026. EU to sprovodi kroz EES, a
prekoračenje znači deportaciju vozača i zadržavanje vozila — zato je tačnost
računanja prioritet broj jedan.

Jedan Go binary servira web UI, Telegram bota i pozadinske podsetnike.
Podaci su u SQLite bazi (WAL). Sve tabele imaju `tenant_id`, pa jedna
instalacija služi više firmi: svi se prijavljuju na istu adresu, a **email
naloga određuje firmu**. Jedan zajednički Telegram bot služi sve firme i po
nalogu koji mu piše zna o kojoj se firmi radi.

> Evidencija služi za internu kontrolu. Zvanično stanje vodi EES sistem na granici.

## Lokalno pokretanje

```bash
cp .env.example .env
make seed ARGS="-tenant-name 'Moja Firma' -email ja@firma.rs -name 'Ime Prezime'"
make run
```

Seed pita za lozinku vlasnika (kuca se dva puta, ne vidi se na ekranu).
Lozinka se ne prosleđuje kao argument, jer bi ostala u istoriji shell-a; za
skripte se može poslati na stdin ili u promenljivoj `SEED_PASSWORD`.

Aplikacija je na `http://localhost:8080/login`.

Svaka sledeća firma je ista komanda sa drugim imenom i emailom — nema DNS-a,
poddomena ni posebnog bota po firmi. Email mora biti jedinstven u celom
sistemu, jer on određuje firmu pri prijavi.

Za prikaz klijentima firma se može popuniti demo vozačima i boravcima
(datumi su relativni u odnosu na danas, pa podaci uvek deluju sveže; `-dry`
samo ispisuje šta bi bilo upisano):

```bash
make demo ARGS="-slug moja-firma"
```

```bash
make test
```

## Produkcija — netcup VPS

Aplikacija radi na netcup VPS-u (Debian 13, x86_64). Spolja je dostupna
samo kroz Cloudflare Tunnel: `cloudflared` na serveru otvara izlaznu vezu ka
Cloudflare-u, a Cloudflare obezbeđuje domen i TLS. Na serveru nema otvorenih
ulaznih portova osim SSH-a, pa ni Caddy-ja.

Aplikacija je na `https://app.dani90.rs`, a glavni domen `dani90.rs` ostaje
za prodajni sajt (`site/`, Cloudflare Pages).

| Šta | Gde |
|---|---|
| program | `/opt/dani90/shengen`, systemd servis `dani90` |
| okruženje | `/etc/dani90/dani90.env` (vlasnik `root:dani90`, `640`) |
| baza | `/var/lib/dani90/dani90.db` |
| tunel | servis `cloudflared`, `/etc/cloudflared/config.yml` |

Aplikacija radi kao sistemski korisnik `dani90` bez prava na login i sluša
samo na `127.0.0.1:8080`. SSH ulaz je samo ključem, root login je isključen,
a `ufw` propušta samo SSH.

### Deploy nove verzije

Na Mac-u:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/shengen-linux ./cmd/server && scp bin/shengen-linux veljko@SERVER_IP:/tmp/
```

Na serveru (prvo kopija baze, jer se migracije primenjuju same pri startu):

```bash
sudo -u dani90 sqlite3 /var/lib/dani90/dani90.db ".backup /var/lib/dani90/pre-izmene.db" && sudo install -m 755 /tmp/shengen-linux /opt/dani90/shengen && sudo systemctl restart dani90
```

Šabloni, statički fajlovi i migracije su ugrađeni u binary, pa se na server
prenosi samo on. SQLite drajver je čist Go, pa build za Linux radi sa Mac-a.

Log i stanje:

```bash
sudo journalctl -u dani90 -n 50 --no-pager
sudo systemctl status cloudflared --no-pager
```

Pri startu aplikacija sama prijavljuje webhook
(`https://app.dani90.rs/telegram/webhook`) Telegramu i to piše u log. Ako
token postoji a secret ne (ili sadrži nedozvoljene znakove), aplikacija se ne
pokreće i kaže zašto.

### Backup

Cron korisnika `dani90` svake noći u 3:30 pravi kopiju baze
(`/var/lib/dani90/backup-<dan u nedelji>.db`, sedam rotirajućih) i ažurira
`backup.ok`. Ako je taj fajl stariji od 26h, operatori dobijaju upozorenje
(`BACKUP_HEARTBEAT_FILE`).

```
30 3 * * * sqlite3 /var/lib/dani90/dani90.db ".backup /var/lib/dani90/backup-$(date +\%u).db" && touch /var/lib/dani90/backup.ok
```

Kopije su na istom disku kao baza. Slanje van servera (Litestream ili R2) je
sledeći korak.

### Okruženje

Pored Telegram i SMTP podešavanja, u `/etc/dani90/dani90.env`:

```
PORT=8080
DB_PATH=/var/lib/dani90/dani90.db
DEV_INSECURE_COOKIES=false
TRUSTED_PROXY=cloudflare
PUBLIC_BASE_URL=https://app.dani90.rs
BACKUP_HEARTBEAT_FILE=/var/lib/dani90/backup.ok
```

`DEV_INSECURE_COOKIES=false` je obavezno — browser razgovara sa Cloudflare-om
preko HTTPS-a, pa sesijski kolačić treba da nosi `Secure` zastavicu.
`TRUSTED_PROXY=cloudflare` kaže aplikaciji da pravu adresu posetioca čita iz
`CF-Connecting-IP`; bez toga svi posetioci dele jedan limit prijava. To je
bezbedno samo dok je tunel jedini put do aplikacije — ako se ikad stavi Caddy
ispred, mora `TRUSTED_PROXY=caddy`, jer Caddy propušta `CF-Connecting-IP` koji
je poslao sam klijent.

### Postavljanje novog servera

Za slučaj da server treba podići ponovo.

**1. Korisnik i folderi**

```bash
sudo apt install -y sudo sqlite3 ufw curl
sudo useradd --system --home /var/lib/dani90 --shell /usr/sbin/nologin dani90
sudo mkdir -p /opt/dani90 /var/lib/dani90 /etc/dani90
sudo chown dani90:dani90 /var/lib/dani90 && sudo chmod 700 /var/lib/dani90
```

**2. systemd servis** — `/etc/systemd/system/dani90.service`:

```ini
[Unit]
Description=Dani90 (evidencija 90/180)
After=network-online.target
Wants=network-online.target

[Service]
User=dani90
Group=dani90
EnvironmentFile=/etc/dani90/dani90.env
WorkingDirectory=/var/lib/dani90
ExecStart=/opt/dani90/shengen
Restart=always
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/dani90

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload && sudo systemctl enable dani90
```

**3. Baza** — sa starog servera `sqlite3 dani90.db ".backup kopija.db"`,
prebaciti u `/var/lib/dani90/dani90.db` (vlasnik `dani90`, `600`).

**4. Tunel** — instalirati `cloudflared` iz Cloudflare-ovog apt repoa, u
`/etc/cloudflared/` staviti credentials fajl tunela `dani90` i config po
[deploy/cloudflared.example.yml](deploy/cloudflared.example.yml), pa
`sudo cloudflared service install`. DNS (`app.dani90.rs` → tunel) se ne
menja. Tunel se pokreće tek kad se stari ugasi, inače Cloudflare deli
saobraćaj između dve instalacije sa dve različite baze.

Novi tunel od nule (samo ako credentials nisu sačuvani):

```bash
cloudflared tunnel login
cloudflared tunnel create dani90
cloudflared tunnel route dns dani90 app.dani90.rs
```

### Telegram bot (jednom, za sve firme)

Kod [@BotFather](https://t.me/BotFather) pošalji `/newbot` i zapamti token i
korisničko ime bota. Secret za webhook: `openssl rand -hex 32`.

Povezivanje:

1. **Podešavanja → Telegram → Generiši link za povezivanje** — otvori link i
   tvoj Telegram postaje administratorski nalog firme.
2. Za vozača: **Vozači → Telegram link** — pošalji mu link (Viber, SMS); on
   samo tapne i povezan je. Link važi 48 sati i jednokratan je.

Webhook može imati samo jedna instalacija. Lokalni `make run` sa produkcijskim
tokenom bi ga preuzeo od servera, pa lokalni `.env` ne sme imati
`TELEGRAM_BOT_TOKEN`.

## Proba izmena pre produkcije

Ne postoji posebno dev okruženje, pa se izmena proba nad kopijom produkcijske
baze. Kopija se povlači sa servera na putanju `DB_PATH` iz lokalnog `.env`
(`make proba` čita `DB_PATH` iz `.env`, ne sa komandne linije):

```bash
ssh veljko@SERVER_IP "sudo -u dani90 sqlite3 /var/lib/dani90/dani90.db '.backup /tmp/kopija.db' && sudo chmod 644 /tmp/kopija.db" && scp veljko@SERVER_IP:/tmp/kopija.db dani90.db && ssh veljko@SERVER_IP "rm /tmp/kopija.db"
make proba
```

`make proba` pravi kopiju `DB_PATH` u `proba/proba.db` i pokreće aplikaciju na
`http://localhost:8095` bez Telegram bota — nijedan vozač ni operator ne
dobija poruku iz kopije — a mejlove samo ispisuje u log (tu se vidi i link za
reset lozinke). `PROBA_SMTP=1 make proba` zadržava SMTP iz `.env`, za probu
pravog slanja; reset tada tražite samo za svoju adresu.

Kopija sadrži lične podatke vozača — obrisati je kad proba završi.

## Struktura

| Putanja | Šta je |
|---|---|
| `internal/calc` | pravilo 90/180 — čiste funkcije, bez I/O |
| `internal/status` | stanje vozača, deljeno između weba i bota |
| `internal/store` | pristup bazi, sve ograničeno na `tenant_id` |
| `internal/web` | stranice, forme, `html/template` + htmx |
| `internal/telegram` | bot: vozačka dugmad i admin komande |
| `internal/jobs` | podsetnici i upozorenja dispečeru |
| `internal/db/migrations` | SQL migracije, primenjuju se pri startu |

## Konfiguracija

| Varijabla | Podrazumevano | Opis |
|---|---|---|
| `PORT` | `8080` | port web servera |
| `BIND_ADDR` | `127.0.0.1` | interfejs na kome server sluša; tunel radi na istom serveru, pa `0.0.0.0` samo ako baš treba |
| `DB_PATH` | `shengen.db` | putanja do SQLite baze |
| `DEV_INSECURE_COOKIES` | `false` | `true` samo za lokalni HTTP razvoj |
| `TRUSTED_PROXY` | `none` | `cloudflare` (tunel), `caddy` ili `none`; odakle se čita adresa klijenta za limit prijava |
| `PUBLIC_BASE_URL` | — | npr. `https://app.dani90.rs`; uz token se webhook prijavljuje sam |
| `TELEGRAM_BOT_TOKEN` | — | token jedinog bota, od @BotFather |
| `TELEGRAM_WEBHOOK_SECRET` | — | obavezan uz token; `openssl rand -hex 32` |
| `TELEGRAM_BOT_USERNAME` | — | opciono, za linkove na koje se samo tapne |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD` | —, `587` | SMTP relej provajdera (Brevo: `smtp-relay.brevo.com`); bez njega na serveru nema slanja mejlova, pa ni reseta lozinke |
| `MAIL_FROM`, `MAIL_REPLY_TO` | — | pošiljalac (obavezan uz SMTP) i adresa za odgovore |
| `MAIL_DAILY_LIMIT` | `250` | najviše mejlova u 24h |
| `TURNSTILE_SITE_KEY`, `TURNSTILE_SECRET_KEY` | — | Cloudflare Turnstile na javnim formama; oba ili nijedan |
| `ANTHROPIC_API_KEY` | — | Uključuje čitanje skenirane evidencije (PDF, slika) na stranici „Uvoz istorije“ preko Claude-a; najviše 10 strana / 10 MB, 10 dokumenata dnevno po firmi |
| `ANTHROPIC_API_KEY_EXPIRES` | — | Poslednji dan važenja ključa (`2026-11-14`); od 5 dana pre isteka operatori svako jutro u 8h dobijaju podsetnik, a `/zdravlje` prikazuje istek |

Token i secret idu samo u okruženje (`/etc/dani90/dani90.env`), nikada u repo. Firma u
**Podešavanja → Telegram** bira samo svoj chat za upozorenja i pragove.

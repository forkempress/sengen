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

## Prvi javni test — Cloudflare Tunnel sa svog računara

Telegram webhook zahteva javni HTTPS, pa aplikacija mora biti dostupna spolja.
Za prvi test nije potreban server: `cloudflared` izlaže aplikaciju sa tvog
računara, a Cloudflare obezbeđuje domen i TLS.

**1. Domen na Cloudflare**

Dodaj `dani90.rs` u Cloudflare i kod registrara postavi Cloudflare-ove
nameservere. Sačekaj da zona postane aktivna.

Aplikacija ide na poddomen `app.dani90.rs`, a glavni domen ostaje slobodan
za kasniji prodajni sajt. Tako se adresa aplikacije (i Telegram webhook) ne
menja kad taj sajt dođe.

**2. Instaliraj i poveži cloudflared**

```bash
brew install cloudflared
cloudflared tunnel login
cloudflared tunnel create dani90
cloudflared tunnel route dns dani90 app.dani90.rs
```

`login` otvara browser i traži da izabereš zonu. `create` ispisuje UUID tunela
i putanju do credentials fajla — oboje ti treba u sledećem koraku.

**3. Konfiguracija tunela**

Prekopiraj [deploy/cloudflared.example.yml](deploy/cloudflared.example.yml) u
`~/.cloudflared/config.yml` i upiši svoj UUID tunela i putanju do credentials
fajla.

**4. Telegram bot (jednom, za sve firme)**

Kod [@BotFather](https://t.me/BotFather) pošalji `/newbot` i zapamti token i
korisničko ime bota. Napravi secret za webhook:

```bash
openssl rand -hex 32
```

**5. Pokreni aplikaciju**

U `.env` postavi:

```
PORT=8080
DB_PATH=dani90.db
DEV_INSECURE_COOKIES=false
TRUSTED_PROXY=cloudflare
PUBLIC_BASE_URL=https://app.dani90.rs
TELEGRAM_BOT_TOKEN=<token od BotFather-a>
TELEGRAM_WEBHOOK_SECRET=<izlaz openssl komande>
TELEGRAM_BOT_USERNAME=<korisničko ime bota, bez @>
```

`DEV_INSECURE_COOKIES=false` je obavezno — browser razgovara sa Cloudflare-om
preko HTTPS-a, pa sesijski kolačić treba da nosi `Secure` zastavicu.
`TRUSTED_PROXY=cloudflare` kaže aplikaciji da pravu adresu posetioca čita iz
`CF-Connecting-IP`; bez toga svi posetioci dele jedan limit prijava.

```bash
make seed ARGS="-tenant-name 'Moja Firma' -email ja@firma.rs -name 'Ime Prezime'"
make run
```

Pri startu aplikacija sama prijavljuje webhook
(`https://app.dani90.rs/telegram/webhook`) Telegramu i to piše u log. Ako
token postoji a secret ne (ili sadrži nedozvoljene znakove), aplikacija se ne
pokreće i kaže zašto.

**6. Pokreni tunel (u drugom terminalu)**

```bash
cloudflared tunnel run dani90
```

Prijavi se na `https://app.dani90.rs/login`.

**7. Povezivanje sa botom**

1. **Podešavanja → Telegram → Generiši link za povezivanje** — otvori link i
   tvoj Telegram postaje administratorski nalog firme.
2. Za vozača: **Vozači → Telegram link** — pošalji mu link (Viber, SMS); on
   samo tapne i povezan je. Link važi 48 sati i jednokratan je.

Dok tunel i aplikacija rade, bot je uživo. Kad ugasiš računar, bot prestaje da
odgovara — zato je ovo privremeno rešenje za test, ne za klijenta.

## Proba izmena pre produkcije

Ne postoji posebno dev okruženje, pa se izmena proba nad kopijom produkcijske
baze, pre nego što se produkcija restartuje sa novim kodom:

```bash
make proba
```

Pravi kopiju `DB_PATH` u `proba/proba.db` (može dok produkcija radi) i
pokreće aplikaciju na `http://localhost:8095` bez Telegram bota — nijedan
vozač ni operator ne dobija poruku iz kopije — a mejlove samo ispisuje u log
(tu se vidi i link za reset lozinke). `PROBA_SMTP=1 make proba` zadržava SMTP
iz `.env`, za probu pravog slanja; reset tada tražite samo za svoju adresu.
Na serveru je potreban paket `sqlite3`.

Pre restarta produkcije sa novim kodom napraviti kopiju baze
(`sqlite3 dani90.db ".backup dani90-pre-izmene.db"`): migracije se primenjuju
same pri startu.

## Kasnije: trajni server

Za produkciju kod ostaje isti, menja se samo okruženje: Hetzner VPS (CX22),
Caddy ispred za automatski TLS, systemd servis i Litestream backup baze na
S3-kompatibilan storage (Cloudflare R2 ili Hetzner Object Storage). Iza
Caddy-ja postaviti `TRUSTED_PROXY=caddy` (adresa iz `X-Forwarded-For`);
`cloudflare` tamo ne sme, jer Caddy propušta `CF-Connecting-IP` koji je
poslao sam klijent.

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
| `BIND_ADDR` | `127.0.0.1` | interfejs na kome server sluša; tunel i Caddy rade na istom računaru, pa `0.0.0.0` samo ako baš treba |
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

Token i secret idu samo u okruženje (`.env`, systemd), nikada u repo. Firma u
**Podešavanja → Telegram** bira samo svoj chat za upozorenja i pragove.

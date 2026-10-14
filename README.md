# Evidencija 90/180

Praćenje dana koje vozači provedu u Šengen zoni, zbog pravila „najviše 90 dana
u bilo kom periodu od 180 dana". Od 2026. EU to sprovodi kroz EES, a
prekoračenje znači deportaciju vozača i zadržavanje vozila — zato je tačnost
računanja prioritet broj jedan.

Jedan Go binary servira web UI, Telegram bota i pozadinske podsetnike.
Podaci su u SQLite bazi (WAL). Sve tabele imaju `tenant_id`, a firma se
prepoznaje po domenu, pa se ista instalacija kasnije prodaje i drugim firmama.

> Evidencija služi za internu kontrolu. Zvanično stanje vodi EES sistem na granici.

## Lokalno pokretanje

```bash
cp .env.example .env
make seed ARGS="-tenant-name 'Moja Firma' -host localhost -email ja@firma.rs -password lozinka -name 'Ime Prezime'"
make run
```

Aplikacija je na `http://localhost:8080/login`.

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

**2. Instaliraj i poveži cloudflared**

```bash
brew install cloudflared
cloudflared tunnel login
cloudflared tunnel create dani90
cloudflared tunnel route dns dani90 dani90.rs
```

`login` otvara browser i traži da izabereš zonu. `create` ispisuje UUID tunela
i putanju do credentials fajla — oboje ti treba u sledećem koraku.

**3. Konfiguracija tunela**

Prekopiraj [deploy/cloudflared.example.yml](deploy/cloudflared.example.yml) u
`~/.cloudflared/config.yml` i upiši svoj UUID tunela i putanju do credentials
fajla.

**4. Pokreni aplikaciju**

U `.env` postavi:

```
PORT=8080
DB_PATH=dani90.db
DEV_INSECURE_COOKIES=false
```

`DEV_INSECURE_COOKIES=false` je obavezno — browser razgovara sa Cloudflare-om
preko HTTPS-a, pa sesijski kolačić treba da nosi `Secure` zastavicu.

```bash
make seed ARGS="-tenant-name 'Moja Firma' -host dani90.rs -email ja@firma.rs -password lozinka -name 'Ime Prezime'"
make run
```

Host u `seed` komandi **mora tačno odgovarati** domenu preko kog se pristupa
(`dani90.rs`), jer se firma prepoznaje po `Host` zaglavlju.

**5. Pokreni tunel (u drugom terminalu)**

```bash
cloudflared tunnel run dani90
```

Prijavi se na `https://dani90.rs/login`.

**6. Telegram bot**

1. Kod [@BotFather](https://t.me/BotFather) komanda `/newbot` → dobijaš token.
2. U aplikaciji: **Podešavanja → Telegram** → nalepi token → **Sačuvaj**
   (webhook secret se generiše sam).
3. Klikni **Postavi webhook** — aplikacija prijavljuje
   `https://dani90.rs/telegram/webhook` Telegramu.
4. **Generiši link za povezivanje** da povežeš svoj Telegram kao administratora.
5. Za vozača: **Vozači → Telegram link**, pa vozač pošalje botu dobijenu
   `/start` komandu.

Dok tunel i aplikacija rade, bot je uživo. Kad ugasiš računar, bot prestaje da
odgovara — zato je ovo privremeno rešenje za test, ne za klijenta.

## Kasnije: trajni server

Za produkciju kod ostaje isti, menja se samo okruženje: Hetzner VPS (CX22),
Caddy ispred za automatski TLS, systemd servis i Litestream backup baze na
S3-kompatibilan storage (Cloudflare R2 ili Hetzner Object Storage). Aplikacija
već čita pravu adresu klijenta i iza Caddy-ja i iza Cloudflare-a.

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
| `DB_PATH` | `shengen.db` | putanja do SQLite baze |
| `DEV_INSECURE_COOKIES` | `false` | `true` samo za lokalni HTTP razvoj |

Telegram token i webhook secret se čuvaju po firmi u bazi (Podešavanja →
Telegram), nikada u repou.

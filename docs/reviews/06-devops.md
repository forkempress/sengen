# DevOps review
Datum: 2026-10-14 | Commit: fd44d81

## Summary
Kod je spreman za VPS: jedan statički binary (`CGO_ENABLED=0 GOOS=linux` build prolazi, 20 MB), migracije su ugrađene u binary, bind je 127.0.0.1, timeout-i i limiti tela postoje, `time/tzdata` je uključen, a `go vet` i `go test -race ./...` prolaze na go1.26.9. Okruženje nije spremno: produkcija je `go run` na laptopu, u `.env` nema `TRUSTED_PROXY` (iza tunela svi posetioci dele jedan limit prijava), backupa nema, a produkcijska baza je na šemi 0013, pa će sledeći start primeniti migracije 0014–0017 bez kopije. Preporuka je Hetzner CX22 + Caddy (DNS-only, bez Cloudflare proxy-ja) + systemd + Litestream na Hetzner Object Storage (EU), uz `healthchecks.io` kao alarm koji ne zavisi od Telegrama. Fly.io ne donosi ništa za jedan SQLite proces, a dodaje prenos u SAD (LEG-08). Ukupno 26 nalaza (4 P0, 13 P1, 9 P2). Novi itemi su AI-57 do AI-60.

## Nalazi
| ID | Naslov | Opis | Dokaz (fajl:linija / URL / screenshot) | Kategorija | Ozbiljnost | Predlog | Napor |
|---|---|---|---|---|---|---|---|
| OPS-01 | Produkcija je `go run` na laptopu | App se pokreće kroz `make run` (`go run`, dev build), bez nadzora procesa i restarta, a radi samo dok je laptop budan. U trenutku review-a `cloudflared tunnel run dani90` radi, a na 127.0.0.1:8080 ništa ne sluša, pa tunel nema kome da prosledi saobraćaj. | Makefile:3-8; `README.md` („Kad ugasiš računar, bot prestaje“); lokalno `curl 127.0.0.1:8080/healthz` bez odgovora, `ps`: cloudflared aktivan | infra | P0 | Hetzner CX22 + Caddy + systemd po napomenama ispod (AI-13). Do selidbe app pokretati u `tmux`/launchd i držati laptop budnim, ili jasno reći pilot klijentu da je app privremena. | M |
| OPS-02 | `TRUSTED_PROXY` nije postavljen u produkcijskom `.env` | U `.env` postoje samo PORT, DB_PATH, DEV_INSECURE_COOKIES, PUBLIC_BASE_URL i tri TELEGRAM_* ključa. Bez `TRUSTED_PROXY` važi `none`. Iza tunela je peer uvek loopback, pa svi posetioci dobijaju isti ključ u IP limiteru: 20 pogrešnih prijava za 15 minuta od bilo koga blokira prijavu svim firmama. Lokalni start bez ove varijable piše `trusted proxy: none`. | `.env` (samo imena ključeva); internal/config/config.go:69-73; internal/auth/clientip.go:53-58; internal/web/server.go:31-32; internal/web/handlers_auth.go:28 | security | P0 | Odmah dodati `TRUSTED_PROXY=cloudflare` (tunel), a na VPS-u `caddy`. U kodu: ako je `PUBLIC_BASE_URL` postavljen, a `TRUSTED_PROXY=none`, odbiti start ili upisati upozorenje (AI-57). | S |
| OPS-03 | Prvi start novog `main`-a migrira produkcijsku bazu bez kopije | `dani90.db` je na migraciji 0013. Sledeći start primenjuje 0014 (prepisuje tabelu `stays`), 0015, 0016 i 0017 (preimenuje `token`→`token_hash`), a backup ne postoji. Na kopiji baze migracija je prošla (`integrity_check` ok, `foreign_key_check` prazan, `stays_cross_tenant` 0 redova, `/healthz` 200). | `sqlite3 dani90.db "select max(filename) from schema_migrations"` → 0013; internal/db/migrations/0014_stays_tenant_driver_fk.sql:15,34; 0017_hash_tokens.sql:6,11; lokalni test na kopiji u scratchpad-u | infra | P0 | Pre prvog starta: `sqlite3 dani90.db ".backup dani90-pre-0014.db"`, kopiju sačuvati i van laptopa (šifrovano). Posle starta proveriti `integrity_check`. Sve prijave se jednom odjavljuju (sesije su heširane), pa korisnike treba upozoriti (AI-58). | S |
| OPS-04 | Nema backupa. Litestream na EU skladište, ne R2 | Baza i WAL postoje samo na disku laptopa. R2 je Cloudflare (SAD), a LEG-08 traži izbegavanje prenosa u SAD. Hetzner Object Storage (fsn1/nbg1/hel1) je ista firma i ista jurisdikcija kao VPS, pa ne dodaje novog obrađivača. | koren repoa (`dani90.db`, `shengen.db`); README „Kasnije: trajni server“; 05-legal.md LEG-08 | infra | P0 | Litestream kao poseban systemd servis → Hetzner Object Storage (ili Hetzner Storage Box preko SFTP-a, ako je jeftinije). Retencija 7 dana, snapshot dnevno. Test vraćanja na čistoj mašini, pa ponavljanje jednom mesečno (AI-14). Konfiguracija i procedura su u napomenama. | M |
| OPS-05 | Migracije samo unapred, rollback binarnog fajla ne radi | `Migrate` pri svakom startu primenjuje fajlove koji nisu zapisani, svaki u svojoj transakciji. Nema down migracija ni kopije pre migracije. Stari binary posle 0017 traži kolonu `token` i pada. Greška u migraciji daje `log.Fatalf`, pa systemd ulazi u petlju restarta. | internal/db/db.go:20-31, 37-87; cmd/server/main.go:30-33; 0017_hash_tokens.sql:6,11 | infra | P1 | Deploy skripta pravi `.backup` pre restarta. Rollback = prethodni binary + ta kopija (gube se upisi posle deploya). Šemu menjati expand/contract (prvo dodaj, posle ukloni). Svaku migraciju pre deploya pustiti na kopiji produkcije. `StartLimitBurst` u unit-u. | S |
| OPS-06 | Caddy iza Cloudflare proxy-ja (narandžasti oblak) kvari IP limiter | Sa `TRUSTED_PROXY=caddy` poslednji element `X-Forwarded-For` je adresa Cloudflare edge-a, pa posetioci dele mali broj ključeva. Sa `cloudflare` app veruje `CF-Connecting-IP`, koji napadač postavlja sam čim zaobiđe Cloudflare i gađa IP VPS-a direktno. Kombinacija nije pokrivena ni kodom ni README-om. | internal/auth/clientip.go:24-28, 59-68; README „Kasnije: trajni server“ | security | P1 | Izabrati jedan režim i zapisati ga u runbook. **A (preporuka):** DNS-only zapis za `app`, Caddy, `TRUSTED_PROXY=caddy`. **B:** cloudflared na VPS-u, bez otvorenih portova, `TRUSTED_PROXY=cloudflare`. Nikad Caddy uz narandžasti oblak bez firewall-a koji pušta samo Cloudflare opsege i `trusted_proxies` u Caddy-ju. | S |
| OPS-07 | Nema uptime alarma, heartbeat-a ni alarma za disk | `/healthz` postoji, ali radi samo `conn.Ping()` (bez timeout-a i verzije) i niko ga ne proverava. Kad VPS, Caddy ili TLS padnu, niko ne saznaje. Baza ima ~150 KB, pa rast nije rizik, ali disk pun zbog journald-a ili Litestream-a jeste. | cmd/server/main.go:54-61 | infra | P1 | systemd timer na 5 min: `curl -fsS https://app.dani90.rs/healthz` (ide kroz DNS, Caddy i TLS), provera `df` < 80%, pa ping na healthchecks.io. Ako ping izostane (VPS pao), healthchecks javlja. Spolja opciono UptimeRobot ili Better Stack na `/healthz` (AI-18). | S |
| OPS-08 | Nema alarma za webhook; alarmi idu samo kroz Telegram | Klijent nema `getWebhookInfo`. Webhook se registruje samo pri startu, a greška se samo loguje. Ako Telegram ili bot ne rade, alarm koji šalje bot ne stiže, pa alarmni kanal ne sme biti samo Telegram. | internal/telegram/client.go:96-115 (nema metode); cmd/server/main.go:102-120 | infra | P1 | Bez promene koda: timer skripta (root, čita token iz env fajla) zove `getWebhookInfo` i javlja `/fail` na healthchecks ako je `url` pogrešan, `last_error_date` mlađi od 15 min ili `pending_update_count` > 20. Healthchecks šalje email i Telegram. Kasnije isto kao job u app-u (AI-18). | S |
| OPS-09 | Upozorenje dispečeru se gubi kad slanje ne uspe | `sendAlert` prvo upiše dedup red (`TryRecordAlert`), pa tek onda šalje. Ako Telegram vrati grešku, red ostaje, a do sutra se isto upozorenje ne šalje ponovo. Na dan pada Telegrama crveno upozorenje za vozača ne stiže uopšte. | internal/jobs/jobs.go:130-145; internal/store/telegram.go:184 | bug | P1 | Na grešku slanja obrisati dedup red (ili ga upisati tek posle uspeha), da sledeći tick od 5 min pokuša ponovo. Dodati test sa lažnim Telegramom koji vraća 500 (AI-60). | S |
| OPS-10 | Rok čuvanja logova nije podešen (LEG-18) | App loguje na stderr. Na VPS-u to ide u journald, čiji je podrazumevani limit samo po veličini, pa rok nije definisan. Logovi sadrže Telegram chat ID. Caddy access log bi dodao IP i URL svakog zahteva. Litestream retencija znači da obrisani podaci ostaju u backupu još N dana. | internal/telegram/bot.go:222, 239, 323; cmd/server/main.go (log.Printf) | legal | P1 | `/etc/systemd/journald.conf.d/dani90.conf`: `MaxRetentionSec=30day`, `SystemMaxUse=500M`. U Caddy-ju bez `log` direktive (access log isključen). U politiku upisati 30 dana za logove i 7 dana za backup (AI-32). | S |
| OPS-11 | Poseban korisnik, dozvole baze i hardening procesa | Na laptopu baza ima 0644 i leži u folderu repoa (`dani90.db`, `shengen.db`). Za VPS nema unit-a, korisnika ni putanje. | `ls -l dani90.db` → `-rw-r--r--`; deploy/ sadrži samo cloudflared primer | security | P1 | Korisnik `dani90` bez shell-a, `StateDirectory=dani90` (0700), `UMask=0077` (baza 0600), `ProtectSystem=strict`, `NoNewPrivileges`, `CapabilityBoundingSet=`. Litestream radi kao isti korisnik. Unit je u napomenama (AI-13). | S |
| OPS-12 | Tajne u produkciji i rotacija | Tajne su TELEGRAM_BOT_TOKEN, TELEGRAM_WEBHOOK_SECRET i Litestream S3 ključevi. Lokalno je `.env` 0600 i van gita (dobro). Rotacija je jednostavna, jer app pri svakom startu ponovo zove `setWebhook` sa trenutnim secret-om, ali procedura nigde nije zapisana. | .gitignore:5; cmd/server/main.go:102-120; internal/config/config.go:75-82 | security | P1 | `/etc/dani90/dani90.env` i `/etc/dani90/litestream.env`, root:root 0600. U unit-u `EnvironmentFile` (systemd ga čita kao root). Netajne vrednosti idu u `Environment=`. Procedura rotacije je u runbook-u (AI-13, AI-59). | S |
| OPS-13 | Osnovno ojačavanje VPS-a | Plan ne pominje SSH, firewall ni ažuriranja. Server sa podacima vozača i jednim adminom treba da se sam ažurira i da ima minimalnu površinu napada. | README „Kasnije: trajni server“ | security | P1 | Hetzner Cloud Firewall: 80/443 svima, 22 samo sa tvoje IP adrese (ili Tailscale). SSH samo ključem, bez root prijave. `unattended-upgrades` sa automatskim restartom u 04:00. Port 8080 nikad javno, jer je bind 127.0.0.1 (AI-13). | S |
| OPS-14 | Deploy je TODO, bez smoke testa i rollback-a | `make deploy` samo ispisuje TODO i pominje kopiranje migracija, a one su već ugrađene u binary (`go:embed`), pa se kopira samo jedan fajl. | Makefile:27-28; internal/db/db.go:14 | infra | P1 | `deploy/deploy.sh <tag>`: cross-build, `scp` u `/opt/dani90/releases/<tag>`, `.backup` baze, `ln -sfn` na `current`, restart, smoke (`/healthz` 200, `/login` 200, `getWebhookInfo.url` tačan). Ako smoke padne, vraća prethodni symlink. Postupak je u napomenama (AI-13). | M |
| OPS-15 | Nema runbook-a za jednu osobu | Vlasnik radi uveče i vikendom. Bez zapisanih koraka za restart, vraćanje backupa, rotaciju tokena i pad Telegrama, svaki incident se rešava ispočetka. | docs/ (nema runbook-a) | infra | P1 | `docs/runbook.md` po skici u napomenama. Test vraćanja i rotacije izvesti jednom po zapisanim koracima (AI-59). | S |
| OPS-16 | Domen ne šalje email, a nije zaštićen od lažnog pošiljaoca | Ako nema SPF-a i DMARC-a (DNS nije proveren), bilo ko može da pošalje „od dani90.rs“ vozačima ili firmama, npr. lažni link za povezivanje. | site/ i README (app ne šalje email); DNS zapisi nisu provereni | security | P1 | Dok nema emaila: `dani90.rs TXT "v=spf1 -all"`, `_dmarc TXT "v=DMARC1; p=reject; adkim=s; aspf=s"`, null MX (`MX 0 .`) ako nema poštanskog sandučeta. Kad dođe transakcioni servis, SPF i DMARC se olabave za taj servis (AI-19, AI-43). | S |
| OPS-17 | Transakcioni email: izbor servisa i DNS | Email je preduslov za AI-44, AI-45 i AI-47. Postmark i SES su američke firme (LEG-08). | 00-action-plan.md AI-43 | infra | P1 | EU servis (Brevo ili Mailjet, FR; Scaleway TEM, FR). Slanje sa poddomena `mail.dani90.rs` (reputacija odvojena od apex-a). SPF `include` servisa, DKIM 2048, DMARC `p=quarantine` sa `rua`, posle 4 nedelje `p=reject`. Test kroz mail-tester i Gmail/Outlook inbox (AI-43). | M |
| OPS-18 | Nema CI-ja | Nema `.github/`. Lokalno prolaze vet, `test -race` (14 paketa, web ~98 s) i cross-build. govulncheck je već u Makefile-u. | Makefile:21-25; nema `.github/workflows` | qa | P2 | Workflow na push i PR: `go vet`, `go test -race -count=1 ./...`, `make vuln`, `CGO_ENABLED=0 GOOS=linux go build`. Na tag: binary kao release artefakt. Deploy iz GitHub-a (SSH ključ u secrets) nije potreban, ručni `deploy.sh` je dovoljan (AI-53). | S |
| OPS-19 | Logovi su tekst bez nivoa | `log.Printf` dodaje sopstveni datum, koji se u journald-u duplira. Nema nivoa ni polja, pa se greške ne mogu filtrirati (`journalctl -p err`). | cmd/server/main.go, internal/jobs/jobs.go, internal/telegram/bot.go (`"log"`) | infra | P2 | `log/slog` sa JSON ili text handler-om bez vremena (journald ga dodaje), nivo `ERROR` za greške. Chat ID ne logovati ili ga heširati (AI-53, LEG-18). | S |
| OPS-20 | HSTS `includeSubDomains` na apex-u i TLS sitnice | Sajt šalje HSTS sa `includeSubDomains` za ceo `dani90.rs`, a app isto za `app.dani90.rs`. To je ispravno, ali svaki budući poddomen mora imati HTTPS bar godinu dana. Caddy sam preusmerava http→https i obnavlja sertifikat. | site/_headers:8; internal/web/headers.go:48 | infra | P2 | Zadržati. `preload` ne dodavati pre stabilnog rada. Dodati CAA zapis (`letsencrypt.org` i, za sajt, Cloudflare-ove CA). Na sajtu uključiti „Always Use HTTPS“ (AI-19). | S |
| OPS-21 | Backup kod istog provajdera kao server | Litestream na Hetzner štiti od pada diska, ali ne od zaključanog ili ugašenog Hetzner naloga, greške u plaćanju ili brisanja bucket-a istim ključem. | — | infra | P2 | Nedeljni `sqlite3 .backup`, šifrovan sa `age`, povući na laptop ili drugi EU provajder. Litestream ključ bez prava brisanja, ako Hetzner to podržava. | S |
| OPS-22 | `TELEGRAM_API_BASE_URL` može da pošalje token na drugi host | Ako se varijabla ikad postavi u produkciji (npr. ostane iz testa), svaki poziv šalje bot token na tu adresu. | internal/telegram/client.go:26-34 | security | P2 | Prihvatati je samo uz `DEV_INSECURE_COOKIES=true` ili na loopback adresi, u suprotnom odbiti start. | S |
| OPS-23 | `/healthz` ne vraća verziju i nema timeout | Smoke test i rollback ne mogu da potvrde koja je verzija pokrenuta. `Ping` na jedinoj konekciji čeka dok je konekcija zauzeta, bez roka. | cmd/server/main.go:54-61; internal/db/db.go:26 | infra | P2 | `PingContext` sa 2 s. Odgovor `ok <verzija> <poslednja migracija>`, verzija preko `-ldflags -X`. | S |
| OPS-24 | Migracije nemaju test | `internal/db` nema testova. Prepisivanja tabele (0011, 0014) uz `foreign_keys(ON)` proverena su samo ovde, ručno na kopiji. | `go test` → `internal/db [no test files]`; 0011:6,24; 0014:15,34 | qa | P2 | Test: migracija prazne baze i fixture-a na šemi 0013 sa podacima, zatim `PRAGMA foreign_key_check` i `integrity_check`. | S |
| OPS-25 | Tunel gađa `localhost`, a app sluša samo 127.0.0.1 | Ako `localhost` razreši prvo na `::1`, prvi pokušaj ne uspe. Radi zbog fallback-a, ali nepotrebno. Važi i ako se tunel zadrži na VPS-u (opcija B). | deploy/cloudflared.example.yml:14; internal/config/config.go:58 | infra | P2 | `service: http://127.0.0.1:8080`. | S |
| OPS-26 | Bez Cloudflare proxy-ja nema WAF-a | U opciji A Cloudflare ne vidi saobraćaj app-a (dobro za LEG-08), ali nema WAF-a ni rate limit pravila. App ima sopstveni limiter prijava, limite tela i timeout-e, a webhook je zaštićen secret-om. | internal/web/headers.go:18-27; internal/web/server.go:31-33; 04-security.md | security | P2 | Dovoljno za početak. Opciono u Caddy-ju propustiti `/telegram/webhook` samo sa Telegram opsega (149.154.160.0/20, 91.108.4.0/22) i proveriti ih pre uključivanja. WAF razmotriti tek uz napade. | S |

## Neprovereno
- Stanje Cloudflare zone (DNS zapisi, SPF/DMARC/MX, „Always Use HTTPS“, WAF, SSL mode) i da li `app.dani90.rs` trenutno vraća 502. Po pravilima nisu slati zahtevi ka produkcijskim ni tunel domenima.
- Vrednosti iz `.env` nisu čitane. Ne zna se da li proces dobija `TRUSTED_PROXY` iz drugog izvora (npr. ručno `export`), pa OPS-02 važi za pokretanje kroz `make run` sa ovim `.env`.
- Proces iz `go run` (pokrenut 13.10.) sluša na `*:9091` (sve interfejse) i nema otvoren `.db` fajl. Nije utvrđeno šta je to.
- Cene i dostupnost: Hetzner je menjao liniju cloud servera i cene, a CX22 i Object Storage cene treba proveriti na dan kupovine. Iznosi u napomenama su okvirni.
- Litestream sintaksa: konfiguracija je data u v0.3 obliku (`replicas:`). Verzija 0.5.x menja deo ključeva, pa proveriti uz instaliranu verziju. Litestream nije instaliran lokalno i nije pokretan.
- Da li Hetzner Object Storage podržava ključ bez prava brisanja (OPS-21).
- Telegram IP opsezi (OPS-26) i ponašanje `getWebhookInfo` uživo nisu provereni.
- Da li je GitHub repo `forkempress/sengen` javan (utiče na minute za Actions i na to šta sme u workflow).
- Caddy, systemd unit i deploy skripta nisu testirani na stvarnom VPS-u. Lokalno su provereni samo cross-build, `go vet`, `go test -race`, migracija kopije baze 0013→0017 i start sa `/healthz` 200.

## Dodatne napomene

### Hosting: poređenje i preporuka
| | Hetzner CX22 + Caddy (preporuka) | Hetzner + cloudflared (opcija B) | Fly.io | Ostati na laptopu |
|---|---|---|---|---|
| Dostupnost | 24/7, restart preko systemd-a | 24/7 | 24/7, ali mašina se seli i restartuje | samo dok je laptop budan |
| TLS | Caddy, Let's Encrypt automatski | Cloudflare | Fly | Cloudflare |
| Otvoreni portovi | 80, 443, 22 (filtriran) | samo 22 | — | — |
| `TRUSTED_PROXY` | `caddy` | `cloudflare` | zavisi od Fly proxy-ja (`Fly-Client-IP`), traži izmenu koda | `cloudflare` |
| LEG-08 | samo EU (Hetzner) | Cloudflare vidi dešifrovan saobraćaj (SAD) | Fly.io Inc. (SAD) | Cloudflare (SAD) |
| SQLite | lokalni disk, Litestream | isto | volume na jednom hostu, Litestream i dalje treba | disk laptopa |
| Operativa | najjednostavnija, sve je na jednoj mašini | + jedan servis više | fly CLI, machines, volumes | nema |

Fly.io ne rešava ništa što jedan VPS ne rešava, a dodaje prenos u SAD i izmenu `clientip.go`. Opcija B je dobar prelazni korak ako hoćeš selidbu bez DNS promene: isti tunel, `TRUSTED_PROXY=cloudflare`, bez Caddy-ja. Ciljno stanje je A.

### systemd: `/etc/systemd/system/dani90.service`
```ini
[Unit]
Description=dani90 (90/180)
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
Type=simple
User=dani90
Group=dani90
# Netajne vrednosti ovde; tajne (TELEGRAM_*) u fajlu 0600 koji systemd čita kao root.
Environment=PORT=8080 BIND_ADDR=127.0.0.1 TRUSTED_PROXY=caddy DB_PATH=/var/lib/dani90/dani90.db PUBLIC_BASE_URL=https://app.dani90.rs
EnvironmentFile=/etc/dani90/dani90.env
ExecStart=/opt/dani90/current/dani90
Restart=on-failure
RestartSec=5
TimeoutStopSec=20
StateDirectory=dani90
StateDirectoryMode=0700
UMask=0077
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
RestrictNamespaces=yes
LockPersonality=yes
SystemCallArchitectures=native
CapabilityBoundingSet=

[Install]
WantedBy=multi-user.target
```
Priprema: `useradd --system --no-create-home --shell /usr/sbin/nologin dani90`; `install -d -m 0755 /opt/dani90/releases`; `install -d -m 0700 /etc/dani90`; env fajlovi `chmod 0600`, vlasnik root.

### Caddy: `/etc/caddy/Caddyfile`
```caddy
{
	email admin@dani90.rs
}

app.dani90.rs {
	encode zstd gzip
	# Bez `log` direktive: access log (IP + URL) se ne piše (LEG-18).
	reverse_proxy 127.0.0.1:8080
}
```
DNS: `app A/AAAA → IP VPS-a`, **DNS only** (siv oblak). Caddy postavlja `X-Forwarded-For` na adresu koju je video, a `clientip.go` čita poslednji element, pa je to ispravno. `CF-Connecting-IP` se ignoriše.

### Litestream
`/etc/litestream.yml` (v0.3 oblik), ključevi u `/etc/dani90/litestream.env` (`LITESTREAM_ACCESS_KEY_ID`, `LITESTREAM_SECRET_ACCESS_KEY`):
```yaml
dbs:
  - path: /var/lib/dani90/dani90.db
    replicas:
      - type: s3
        bucket: dani90-backup
        path: dani90
        endpoint: https://fsn1.your-objectstorage.com
        region: fsn1
        retention: 168h
        snapshot-interval: 24h
        sync-interval: 1s
```
Drop-in `systemctl edit litestream`:
```ini
[Unit]
After=dani90.service
[Service]
User=dani90
Group=dani90
EnvironmentFile=/etc/dani90/litestream.env
ReadWritePaths=/var/lib/dani90
```
Uz to: Hetzner Backups opcija za VPS (dnevni snimak diska) kao druga linija, i OPS-21 kao treća.

**Test vraćanja** (pre gašenja laptopa, posle toga jednom mesečno, rezultat upisati u runbook):
1. Na drugoj mašini (privremeni CX22 ili laptop) instalirati istu verziju Litestream-a, isti `litestream.yml` i ključeve.
2. `litestream restore -config /etc/litestream.yml -o /tmp/restore.db /var/lib/dani90/dani90.db`
3. `sqlite3 /tmp/restore.db "PRAGMA integrity_check; PRAGMA foreign_key_check; select max(filename) from schema_migrations; select count(*) from stays; select count(*) from drivers;"` i uporediti brojeve sa produkcijom.
4. `env -i DB_PATH=/tmp/restore.db PORT=18080 ./dani90` (bez bot tokena), `curl 127.0.0.1:18080/healthz` → `ok`, prijava test nalogom.
5. Zapisati RPO (koliko sekundi podataka fali) i RTO (koliko minuta je trajalo). Obrisati `/tmp/restore.db`.

**Migracije i rollback:** pre svakog deploya `sqlite3 /var/lib/dani90/dani90.db ".backup /var/lib/dani90/pre-<tag>.db"` (čuvati poslednja 3). Ako nova verzija ima migraciju i mora nazad, zaustaviti app, vratiti tu kopiju i prethodni binary. Upisi posle deploya se gube, pa rollback raditi brzo i javiti korisnicima.

### Monitoring
- healthchecks.io (EU): check „dani90-up“ sa periodom 5 min i grace 10 min, integracije email + Telegram.
- `/usr/local/bin/dani90-check` (root, timer na 5 min):
  1. `curl -fsS --max-time 10 https://app.dani90.rs/healthz`;
  2. `df --output=pcent / /var/lib/dani90` < 80%;
  3. `getWebhookInfo` (token iz `/etc/dani90/dani90.env`): `url == https://app.dani90.rs/telegram/webhook`, `last_error_date` starije od 15 min, `pending_update_count` ≤ 20.

  Ako je sve u redu, ping `https://hc-ping.com/<uuid>`, inače `/fail` sa razlogom u telu. Token se ne ispisuje.
- journald: `/etc/systemd/journald.conf.d/dani90.conf` sa `[Journal]`, `SystemMaxUse=500M` i `MaxRetentionSec=30day`, pa `systemctl restart systemd-journald`.
- Spolja opciono: UptimeRobot ili Better Stack, besplatni, na `/healthz` (bez ličnih podataka).

### Deploy (skica `deploy/deploy.sh <tag>`)
1. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=<tag>" -o dist/dani90 ./cmd/server`
2. `scp dist/dani90 vps:/opt/dani90/releases/<tag>/dani90`
3. `ssh vps`: `.backup` baze → `ln -sfn releases/<tag> /opt/dani90/current` → `systemctl restart dani90`
4. Smoke: `/healthz` 200, `GET /login` 200, `getWebhookInfo.url` tačan, bez `last_error_date` posle restarta.
5. Ako smoke ne prođe: `ln -sfn releases/<prethodni>` + restart. Ako je bila migracija, vratiti i `pre-<tag>.db` (videti gore).

CI (`.github/workflows/ci.yml`): `actions/setup-go` sa `go-version-file: go.mod`, `go vet ./...`, `go test -race -count=1 ./...`, `make vuln`, cross-build. Deploy ostaje ručni.

### Runbook (sadržaj za `docs/runbook.md`)
- **Restart:** `systemctl restart dani90`; logovi `journalctl -u dani90 -n 200`; greške `journalctl -u dani90 -p err --since today`.
- **Vraćanje backupa:** `systemctl stop dani90 litestream` → `mv` baze, `-wal` i `-shm` u `/var/lib/dani90/broken-<datum>/` → `litestream restore ...` → `chown dani90:dani90`, `chmod 0600` → `start dani90`, pa `start litestream` (Litestream počinje novu generaciju) → smoke.
- **Rotacija bot tokena:** @BotFather `/revoke` → novi token u `/etc/dani90/dani90.env` → `systemctl restart dani90`. App sama ponovo prijavljuje webhook (cmd/server/main.go:102-120) → proveriti `getWebhookInfo`. Raditi odmah ako token procuri i jednom godišnje.
- **Rotacija webhook secret-a:** `openssl rand -hex 32` → env → restart. Update-i potpisani starim secret-om dobijaju 401, a Telegram ih ponavlja sa novim.
- **Rotacija S3 ključa:** novi ključ u Hetzner konzoli → `litestream.env` → `restart litestream` → obrisati stari ključ.
- **Telegram ne radi:** web radi normalno. Dispečerima javiti da prate „Pregled“ na webu. Posle oporavka Telegram isporučuje zaostale update-e do 24h. Upozorenja tog dana se trenutno gube (OPS-09).
- **VPS ne radi:** Hetzner status → rescue ili novi CX22 iz snimka, ili čist server + restore iz Litestream-a. DNS za `app` promeniti ako se promeni IP (TTL 300).
- **Disk pun:** `journalctl --vacuum-size=200M`, proveriti `/var/lib/dani90/pre-*.db`.

### Troškovi mesečno (okvirno, bez PDV-a, proveriti cenovnik)
| Stavka | € |
|---|---|
| Hetzner CX22 (sa IPv4) | ~4–6 |
| Hetzner Backups (20% cene servera) | ~1 |
| Hetzner Object Storage (osnovna mesečna cena) ili Storage Box BX11 | ~4–6 |
| Domen `.rs` (godišnje / 12) | ~1–2 |
| Cloudflare (DNS + Workers sajt), healthchecks.io, GitHub Actions, EU email servis (besplatni nivo) | 0 |
| **Ukupno** | **~10–15** |

### Status infra itema iz action plana
| Item | Odluka | Šta je promenjeno |
|---|---|---|
| AI-10 | Potvrđeno, urađeno u kodu (7a1e40a): timeout-i, MaxBytesReader, bind 127.0.0.1 | Oznaka uklonjena. Dodat OPS-25 |
| AI-13 | Potvrđeno, prošireno | Kriterijum: opcija A, firewall, SSH, deploy skripta sa smoke i rollback (OPS-01, 05, 06, 11–14) |
| AI-14 | Potvrđeno: Hetzner Object Storage, ne R2 | Kriterijum: retencija, mesečni test vraćanja, RPO/RTO (OPS-04, 21) |
| AI-18 | Potvrđeno, dopunjeno | Alarm van Telegrama (healthchecks.io), prvo kao skripta bez koda (OPS-07, 08) |
| AI-19 | Potvrđeno, dopunjeno | HSTS već postoji. Dodati CAA, SPF/DMARC za domen bez emaila (OPS-16, 20) |
| AI-32 | Potvrđeno za infra deo | journald 30 dana, Caddy bez access loga, rok backupa u politici (OPS-10) |
| AI-43 | Potvrđeno | EU servis, poddomen, DMARC postepeno (OPS-17) |
| AI-53 | Delimično osporeno | CI da. Deploy na tag iz GitHub-a ne treba, ručni `deploy.sh` je dovoljan. Smoke i rollback prebačeni u AI-13 (OPS-14, 18, 19) |
| novo | AI-57 (P0), AI-58 (P0), AI-59 (P1), AI-60 (P1) | OPS-02, OPS-03, OPS-15, OPS-09 |

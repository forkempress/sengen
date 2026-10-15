# Pregled ličnih podataka (stanje koda 15.10.2026)

Izvor: migracije `internal/db/migrations/0001–0022`, `internal/store`,
`internal/jobs/jobs.go` (dnevno čišćenje), `internal/auth`. Služi kao osnova
za `/privatnost`, `/uslovi` i `/obrada-podataka`; pri izmeni šeme ažurirati
i ovaj pregled i te stranice.

## Podaci vozača (firma je rukovalac, Dani90 obrađivač)

| Tabela | Podaci | Svrha | Rok / brisanje | Ko vidi |
|---|---|---|---|---|
| `drivers` | ime, oznaka vozila, Telegram chat ID, aktivan, `created_at`, `privacy_ack_at` | evidencija, bot | do trajnog brisanja (vlasnik) ili brisanja firme; deaktivacija briše samo Telegram vezu | vlasnik i dispečeri firme |
| `stays` | datum ulaska/izlaska, posao/privatno, izvor, napomena, ko je uneo | proračun 90/180 | isto; nema automatskog brisanja | isto; vozač svoje u botu (`/moji_podaci`) |
| `planned_stays` | datumi plana, napomena | planiranje | isto | vlasnik i dispečeri |
| `telegram_alert_log` | ID vozača, vrsta upozorenja, datum | da se upozorenje ne šalje dvaput | isto; nema automatskog brisanja | samo sistem |
| `telegram_link_tokens` | heš tokena, ID vozača/korisnika | povezivanje sa botom | važi 48 h; dnevno čišćenje briše istekle | samo sistem |
| `events` | lokacija (lat/lng, država) | **ne koristi se** — tabela postoji, kod ništa ne upisuje | — | — |
| `stays_cross_tenant` | karantin boravaka iz migracije 0014 | pregled falsifikovanih unosa | ručno | operater |

## Podaci firmi i korisnika (Dani90 je rukovalac)

| Tabela | Podaci | Rok / brisanje |
|---|---|---|
| `tenants` | naziv, oznaka, podešavanja (ID grupe za upozorenja, pragovi), plan, probni period, cena po vozaču, prihvaćena verzija uslova, datum, ko je prihvatio | do brisanja firme (ručno, `docs/ops/brisanje-firme.md`) |
| `users` | email, bcrypt heš lozinke, uloga, ime, Telegram chat ID, aktivan | deaktivacija ne briše red (veze u evidenciji izmena) |
| `sessions` | heš tokena, CSRF tajna, vreme isteka i poslednje aktivnosti, flash poruka | 14 dana apsolutno / 24 h neaktivnosti; dnevno čišćenje |
| `audit_log` | ko (ID korisnika/vozača), šta, kada; kopije pre/posle za korisnike i planove | nema automatskog brisanja; trajno brisanje vozača briše kopije sa njegovim datumima |
| `telegram_updates` | ID Telegram poruke, vreme prijema | 7 dana; dnevno čišćenje |
| `superadmin_audit_log` | Telegram ID operatera, komanda, argumenti | nema automatskog brisanja |
| `superadmin_notifications` | vrsta, referenca (ID firme ili datum) | nema automatskog brisanja |

## Van baze

- **Logovi servera** (stderr → journald): greške sa ID-jem vozača/firme i
  Telegram chat ID-jem. Rok: [POPUNITI].
- **Backup** (Cloudflare R2): cela baza. Rok: [POPUNITI].
- **Telegram**: sadržaj poruka bota (ime, datumi, broj dana) prolazi kroz
  Telegram.
- **Sajt dani90.rs**: forma za demo šalje ime, firmu, telefon, email i poruku
  servisu Web3Forms, koji ih prosleđuje emailom. Kalkulator čuva datume samo
  u `localStorage` pregledača. Nema analitike ni kolačića u kodu sajta
  (CSP dozvoljava samo sopstvene fajlove i Web3Forms).

## Kolačići

Aplikacija: samo kolačić sesije (`HttpOnly`, `Secure`, `SameSite=Lax`).
CSRF token je u sesiji na serveru i u skrivenom polju forme, ne u kolačiću.
Baner za kolačiće nije potreban.

## Lokacija

Ne obrađuje se. `stays.source` dozvoljava vrednost `telegram_location`, a
tabela `events` ima kolone za koordinate, ali ih nijedan deo koda ne upisuje
(`internal/telegram/bot.go`: „Live-location geofencing is a later phase“).
Uvođenje traži izmenu politike, ugovora o obradi i posebno obaveštenje
vozaču.

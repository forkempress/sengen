# Samostalna registracija (self-signup) — analiza i plan

Stanje: 15.10.2026, `main` @ bf5d296. Dokument je plan, kod još nije menjan.
Veza sa akcionim planom: AI-17 (zaštita signupa), AI-43 (email), AI-44 (reset i
pozivnice), AI-45 (registracija i wizard), AI-47 (trial), AI-49 (CTA na sajtu),
SEC-07, SEC-08.

---

## 1. Trenutno stanje

### 1.1 Firme i nalozi

| Šta | Gde | Napomena |
|---|---|---|
| Šema `tenants`, `users` | `internal/db/migrations/0001_tenants_users.sql:1-19` | `users.role` CHECK `owner`/`dispatcher` (:15), `password_hash NOT NULL` (:13) |
| Email je jedinstven u celom sistemu | `0013_login_by_email.sql:16` | email bira firmu; `tenants.host` → `slug` (:11) |
| Plan, trial, cena | `0018_billing_superadmin.sql:7-11` | `plan` trial/paid/paused, `trial_until` (YYYY-MM-DD, Beograd), `price_per_driver_cents` default 200; „read-only u v1, menja ih samo cmd/seed“ |
| Prihvatanje uslova već postoji | `0022_terms_privacy.sql:5-7` | `terms_version`, `terms_accepted_at`, `terms_accepted_by` na `tenants` |
| `users.active` | `0020_users_active.sql` | deaktivacija umesto brisanja |
| Kreiranje firme | `internal/store/tenant.go:68-79` | `trial_until = date('now','+30 days')` — **UTC**, ne Beograd; slug UNIQUE, bez automatskog sufiksa |
| Slug iz imena | `internal/store/tenant.go:86-89` | `SlugFromName` |
| Kreiranje korisnika | `internal/store/user.go:74-93` | normalizuje email (:70-72), duplikat → `ValidationError` (:85-86) |
| `Tenant` struct ne nosi plan/trial | `internal/store/tenant.go:18-25` | plan se čita samo kroz `TenantOverview` (`store/superadmin.go:100-160`) |
| Jedini način pravljenja firme | `cmd/seed/main.go:37-45, 86, 112, 148` | `-tenant-name -email -name`, lozinka sa stdin; naplata preko `SetTenantBilling` |

### 1.2 Prijava, sesije, zaštite

| Šta | Gde |
|---|---|
| Rute: `GET/POST /login`, `/logout`, javne pravne strane | `internal/web/server.go:49-59` |
| `POST /login` iza `http.NewCrossOriginProtection` (nema sesije za CSRF) | `server.go:50-53` |
| Lanac zaštićenih ruta `RequireAuth → CSRFProtect → requireTerms` | `server.go:108` |
| Login handler: oba limita pre bcrypt-a, ista poruka za nepoznat email i pogrešnu lozinku, `SpendPasswordCheck` protiv tajminga | `internal/web/handlers_auth.go:23-63`, `internal/auth/password.go:33-35` |
| Rate limiter: in-memory sliding window, cap 10 000 ključeva, atomski `Take` | `internal/auth/ratelimit.go:20-73` |
| Limiti: 20/15 min po IP, 10/15 min po nalogu | `internal/web/server.go:29-33` |
| IP klijenta po `TRUSTED_PROXY` | `internal/auth/clientip.go`, `internal/config/config.go:27-31` |
| Sesija: 14 dana, idle 24h, kolačić HttpOnly/Secure/Lax, u bazi SHA-256 heš | `internal/auth/session.go:12-57`, `0017_hash_tokens.sql`, `internal/store/tokenhash.go:11-14` |
| CSRF: synchronizer token po sesiji | `internal/auth/csrf.go:16-22`, `internal/auth/middleware.go:99-117` |
| Uloge | `internal/auth/middleware.go:79-94`; podesavanja/korisnici i Telegram podešavanja su samo za `owner` (`server.go:89-98`) |
| Firma se uvek uzima iz sesije | `internal/auth/middleware.go:38-75` |
| CSP: samo `'self'` (Turnstile bi bio blokiran) | `internal/web/headers.go:34-38` |
| Flash u sesiji, ne u URL-u | `0019_session_flash.sql`, `internal/web/render.go:28-60` |

### 1.3 Postoji li `/signup`?

**Ne.** Nema rute, šablona ni stuba (`grep signup|registr` po `internal/` i `cmd/` ne
nalazi ništa osim teksta „Firma je registrovana“ u `pregled.html:21`). Sajt nudi samo
„Zakažite demo“ (Web3Forms) — `site/index.html:75, 89, 300-352` — i link „Prijava“
(`site/index.html:74`, `site/kalkulator.html:74`).

### 1.4 Dispečeri i lozinke

- **Dodavanje dispečera:** vlasnik unosi ime, email, **lozinku** i ulogu
  (`internal/web/handlers_users.go:96-132`), pa poruka kaže „Javite mu email i
  lozinku“ (:130). Validacija `handlers_users.go:32-58`, `minPasswordLen = 10` (:19),
  max 72 bajta (:23).
- **Enumeracija (SEC-08):** „Korisnik sa tim emailom već postoji“
  (`handlers_users.go:117-123`) otkriva vlasniku bilo koje firme da adresa postoji
  u drugoj firmi, jer je email globalno jedinstven.
- **Promena lozinke:** postoji (`internal/web/handlers_password.go:40-87`), traži
  trenutnu lozinku, broji se u limit naloga; `SetUserPassword` briše sve sesije i
  otkači Telegram (`internal/store/user.go:182-194`, `revokeUserAccess` :212-240).
- **Reset lozinke:** **ne postoji** — nema rute. `login.html:26` kaže „Vlasnik naloga
  u vašoj firmi može da vam postavi novu“, ali stranica korisnika nudi samo
  deaktivaciju, aktivaciju i ulogu (`server.go:94-98`), ne novu lozinku. Vlasnik koji
  zaboravi lozinku nema izlaz osim `cmd/seed`. PR 1 ispravlja i ovaj tekst.

### 1.5 Prazna stanja posle registracije

Kod je već pripremljen za firmu bez ičega:

- Pregled ima checklistu „Još N koraka do prvog pregleda“: firma registrovana ✓ →
  dodaj vozače → unesi boravke → poveži Telegram (`internal/web/templates/pregled.html:17-35`,
  `internal/web/handlers_dashboard.go:53-64, 155-162`). Testovi: `internal/web/empty_state_test.go`.
- Upozorenje „Upozorenja se ne šalju nikome“ prikazuje se tek kad ima vozača
  (`handlers_dashboard.go:155`) — nova firma ga ne vidi odmah, dobro.
- Firma bez podešavanja: `settings = '{}'` (`tenant.go:70`), pragovi imaju default
  (`internal/jobs/jobs.go:198`), alert chat 0 = nema primaoca.
- `requireTerms` vlasnika bez prihvaćenih uslova šalje na `/prihvatanje`
  (`internal/web/handlers_legal.go:44-66`). Signup mora odmah upisati prihvatanje,
  inače bi nov korisnik odmah posle potvrde video još jedan ekran.
- Vozači se prave na **tri mesta**: web (`handlers_settings.go:67`), bot `/dodaj`
  (`internal/telegram/admin.go:129`) i dugme u botu (`internal/telegram/admin_callback.go:95`);
  svi idu kroz `store.CreateDriver` (`internal/store/driver.go:92-108`). Reaktivacija:
  `store.SetDriverActive` (`driver.go:112-125`). Limit 10 vozača zato pripada store sloju.
- SQLite radi sa jednom konekcijom (`internal/db/db.go:36`), pa je „prebroj pa
  ubaci“ u transakciji bez trke.

### 1.6 Telegram link tokeni (uzor za email tokene)

`telegram_link_tokens` (`0005`, `0017`): u bazi samo SHA-256 heš, novi token
poništava stare neiskorišćene (`internal/store/telegram.go:33-61`), potrošnja je jedan
`UPDATE … WHERE used_at IS NULL AND expires_at > ? RETURNING` (:66-99), razlikuje
„iskorišćen“ i „istekao“, čišćenje u `jobs.Cleanup` (`internal/jobs/jobs.go:100-114`).
Tabela ima CHECK „tačno jedno od driver_id/user_id“ (`0005:8`), pa se **ne može**
proširiti na email — treba nova tabela po istom obrascu.

### 1.7 Superadmin bot

- Komande: `/admin /stats /mrr /firme /firma /trial /neaktivne /zdravlje`
  (`internal/superadmin/superadmin.go:55`) — sve **read-only** (`superadmin.go:2`).
  `/trial` (`commands.go:292`), `/neaktivne` — 14+ dana bez aktivnosti (`commands.go:20-21, 326-360`).
- Push (`internal/superadmin/notifier.go`), tick iz job runnera na 5 min (`jobs.go:24`),
  dedup u `superadmin_notifications` (`0018:31-36`):
  - `new_tenant` za firmu mlađu od 24h (`notifier.go:68-74`) — **već radi za signup**, samo bez izvora;
  - `trial_expiring` operaterima 3 dana pre isteka (`notifier.go:35-36, 75-81`) — owner ne dobija ništa;
  - `first_activity` — prvi boravak nove firme (`notifier.go:82-85`);
  - dnevni digest u 8h (`notifier.go:132`).
- Produženje triala: **nema**; jedino `cmd/seed -slug X -trial-until …` (`cmd/seed/main.go:128-156`).

### 1.8 Email i pravni tekstovi

- App ne šalje email (nema `internal/mail`, nema SMTP/API ključeva u `config.go:18-52`).
- Politika privatnosti kaže „aplikacija koristi samo jedan kolačić“ i „sajt ne
  postavlja kolačiće, nema analitike“ (`internal/web/templates/privatnost.html:45`);
  email provajder je `[POPUNITI]` (`privatnost.html:55`, `obrada_podataka.html:23`).
  Signup (kolačić za čekanje potvrde, UTM, Turnstile, email provajder) zahteva
  izmenu ovih tekstova.
- `legal.TermsVersion = "2026-10-15"` (`internal/legal/legal.go:19`).

---

## 2. Izbor varijante

| | UX trenje | Bezbednost | Napor | Zavisnosti |
|---|---|---|---|---|
| **A** email + sopstvena lozinka + potvrda mejlom | srednje: jedno polje više; potvrda zahteva skok u mejl | dobra: bcrypt postoji, reset preko istog tokena; rizik pre-preuzimanja naloga rešiv (vidi 3.3) | srednji — login, sesije, limiti već postoje | email provajder, Turnstile |
| **B** magic link bez lozinke | malo pri registraciji, **veliko svakog dana**: svaka prijava = mejl; na deljenom PC-ju dispečer otvara mejl; link iz FB in-app browsera se otvara u drugom pregledaču | dobra, ali sesija = posed mejla; skeneri linkova (Outlook Safe Links) troše GET linkove | srednji, ali menja login koji radi | email mora raditi **za svaku prijavu** — pad provajdera = niko se ne prijavljuje |
| **C** email + generisana lozinka u mejlu | malo pri registraciji, pa „promenite lozinku“ | loša: lozinka u čistom tekstu u sandučetu, zauvek | mali | email |
| **D** Telegram Login Widget | malo ako je owner u Telegramu na tom uređaju; widget otvara popup ka oauth.telegram.org, što u FB/Instagram in-app pregledaču često puca | dobra (HMAC provera) | srednji (`/setdomain`, provera heša) | Telegram; **nema emaila** → nema kontakta za fakturu, podsetnike, reset; vezuje identitet vlasnika firme za privatni Telegram nalog |

**Preporuka: A, uz tri izmene** (tvoja polazna preporuka stoji, ali u čistom obliku
ima problem baš sa tvojim kanalom — Facebook reklame na telefonu):

1. **Mejl nosi i link i 6-cifreni kod.** Korisnik sa FB reklame popunjava formu u
   Facebook in-app pregledaču. Link iz mejla otvara se u Gmail aplikaciji ili
   Chrome-u — drugi pregledač, druga kolačići — i originalni tab ostaje zaglavljen na
   „proverite mejl“. Kod se ukuca u isti tab (`autocomplete="one-time-code"`,
   `inputmode="numeric"`) i nastavlja se bez skoka. Link ostaje za desktop i za one
   koji su zatvorili tab. Akcioni plan (UX-30) je već predlagao kod.
2. **Link ne potvrđuje GET-om.** `GET /potvrda?t=…` prikazuje „Potvrdi nalog za
   firmu «X»“ i dugme (POST). Skeneri linkova u korporativnim sandučićima rade GET i
   inače bi potrošili jednokratni token.
3. **Link u tuđem pregledaču ne pravi sesiju sam.** Ako potvrda stigne iz pregledača
   koji nema kolačić čekanja (vidi 3.3), nalog se potvrdi i traži se lozinka
   (login sa unapred upisanim emailom). Tako onaj ko je registrovao tuđi email ne
   dobija ništa time što žrtva klikne link.

D ima smisla kasnije kao **dodatni način prijave** za već postojeće naloge (AI-48 B),
ne kao registracija.

---

## 3. Dizajn

### 3.1 Tok

```
dani90.rs (FB reklama, UTM) ──CTA "Probajte besplatno 30 dana"──▶ app.dani90.rs/registracija?utm_…

/registracija  (GET forma, POST iza CrossOriginProtection + Turnstile)
  polja: naziv firme · ime i prezime · email · lozinka (min 10) · telefon (opciono)
         ☐ Prihvatam Uslove korišćenja i Ugovor o obradi podataka; pročitao sam Politiku privatnosti
  hidden: utm_source/medium/campaign/content/term, ref, landing
        │
        ▼  uvek ista strana, bez obzira da li email postoji
/registracija/proverite-mejl   „Poslali smo mejl na m***@firma.rs. Unesite kod ili kliknite link.“
  [______] Potvrdi   ·   Pošalji ponovo (posle 60 s)   ·   Pogrešan email? Počni ispočetka
        │
        ├─ kod u istom tabu ───────────────┐
        └─ link (GET → strana sa dugmetom → POST)
                                           ▼
                transakcija: tenant (trial_until = danas+30 Beograd, plan trial,
                terms_* iz forme, izvor) + user owner → sesija (ako je isti pregledač)
                → push superadminu „🆕 Nova firma … izvor: facebook / kampanja X“
                                           ▼
/dobrodosli (onboarding, 3 koraka, može se preskočiti)
  1. Dodaj prvog vozača (ime, registracija)
  2. Link i QR za Telegram (postojeći issueDriverLink, handlers_telegram.go:156)
     + „Povežite i svoj Telegram za upozorenja i podsetnike“
  3. Prvi boravak (ili CSV) → /pregled sa postojećom checklistom
```

**Pre potvrde emaila korisnik ne može ništa** osim strane „proverite mejl“, unosa
koda, „pošalji ponovo“ i „počni ispočetka“. Ne postoje ni tenant ni user dok
mejl nije potvrđen — zato nema ni Telegram linkova, ni vozača, ni push-a
superadminu za nepotvrđene (to je SEC-07: „verifikacija pre slanja Telegram
linkova“).

### 3.2 Email tokeni (jedan mehanizam za tri namene)

Nova tabela po uzoru na `telegram_link_tokens`:

```sql
CREATE TABLE email_tokens (
    token_hash  TEXT PRIMARY KEY,             -- SHA-256 linka (store.hashToken)
    purpose     TEXT NOT NULL CHECK (purpose IN ('signup', 'reset', 'invite')),
    email       TEXT NOT NULL,                -- normalizovan
    tenant_id   INTEGER REFERENCES tenants(id), -- reset, invite
    user_id     INTEGER REFERENCES users(id),   -- reset
    code_hash   TEXT,                         -- signup: SHA-256(token_hash || kod); NULL za ostale
    code_tries  INTEGER NOT NULL DEFAULT 0,   -- posle 5 pogrešnih token je mrtav
    browser_hash TEXT,                        -- signup: heš kolačića čekanja (3.3)
    payload     TEXT NOT NULL DEFAULT '{}',   -- signup: firma, ime, telefon, bcrypt heš, terms, izvor; invite: ime, uloga, ko poziva
    expires_at  TEXT NOT NULL,
    used_at     TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_email_tokens_email ON email_tokens(purpose, email);
CREATE INDEX idx_email_tokens_tenant ON email_tokens(tenant_id, purpose);
CREATE INDEX idx_email_tokens_expires ON email_tokens(expires_at);
```

| Namena | Trajanje | Izdavanje poništava | Potrošnja |
|---|---|---|---|
| `signup` | 24h | ranije neiskorišćene `signup` za isti email | kod (5 pokušaja) ili link → pravi firmu i vlasnika |
| `reset` | **1h** (predlog; 24h je predugo za preuzimanje naloga) | ranije `reset` za tog korisnika | postavlja lozinku (`SetUserPassword` već briše sesije) |
| `invite` | 7 dana (dispečer ne čita mejl istog dana) | raniju pozivnicu za isti email u istoj firmi | dispečer bira lozinku → pravi `users` red |

Potrošnja: isti atomski `UPDATE … SET used_at=? WHERE token_hash=? AND purpose=? AND
used_at IS NULL AND expires_at>? RETURNING …` kao `ConsumeLinkToken`
(`store/telegram.go:66-99`). Kod se proverava u konstantnom vremenu i povećava
`code_tries` u istoj transakciji. Čišćenje u `jobs.Cleanup`. Linkovi idu sa
`Referrer-Policy: no-referrer` na stranama sa tokenom.

Payload u JSON-u (umesto posebne `signup_requests` tabele) jer se čita samo pri
potrošnji; bcrypt heš lozinke je jedini osetljiv podatak u njemu i nestaje u
čišćenju posle 24h.

### 3.3 Pre-preuzimanje naloga i kolačić čekanja

Napad: neko registruje `vlasnik@firma.rs` sa svojom lozinkom; žrtva dobije mejl i
klikne. Odbrana:

- POST `/registracija` postavlja kolačić `d90_signup` (HttpOnly, Secure, Lax, 24h,
  nasumičan, u bazi samo heš u `browser_hash`).
- Kod radi **samo** uz taj kolačić (isti tab).
- Link: ako kolačić odgovara → potvrda + sesija. Ako ne → potvrda bez sesije i
  „Prijavite se lozinkom koju ste izabrali“ (email unapred upisan). Strana potvrde
  prikazuje naziv firme i ime i kaže „Niste se vi registrovali? Ne potvrđujte.“
- Ako napadač ipak dobije potvrđen nalog na tuđi email, vlasnik sandučeta ga preuzima
  resetom (reset briše sesije).

### 3.4 Bez enumeracije

| Situacija | Šta vidi posetilac | Šta stiže na email |
|---|---|---|
| nov email | „Poslali smo mejl…“ | kod + link |
| email već ima nalog (bilo koja firma) | **isto** | „Već imate nalog na Dani90. Prijava · Zaboravili ste lozinku?“ |
| email ima neiskorišćen signup | isto | nov kod + link (stari prestaju da važe) |
| reset za nepostojeći/deaktiviran nalog | „Ako nalog postoji, poslali smo link“ | ništa |
| pozivnica na email koji već postoji | vlasnik vidi „Pozivnica poslata“ | pozvani dobija „Ova adresa već ima nalog u drugoj firmi; jedan email = jedna firma. Pišite nam ili koristite drugu adresu.“ |

U oba signup slučaja handler radi istu skupu operaciju (bcrypt lozinke) i šalje
tačno jedan mejl, pa ni vreme odgovora ne otkriva razliku. Pozivnice ujedno
rešavaju SEC-08 (`handlers_users.go:117-123`).

### 3.5 Zaštita

- **Turnstile** na `POST /registracija` i `POST /zaboravljena-lozinka` (kasnije i
  posle 3 loša logina). Serverska provera `siteverify` sa `remoteip`, proverava i
  `hostname` i `action`. CSP samo na te dve strane dobija
  `script-src 'self' https://challenges.cloudflare.com; frame-src https://challenges.cloudflare.com`
  (`headers.go:34-38` postaje funkcija po ruti). Dev koristi Cloudflare-ove javne test
  ključeve (uvek prolaze). Besplatan plan: do 20 widgeta, neograničeno izazova.
- **Rate limit** — nove instance postojećeg `auth.LoginLimiter` (in-memory je dovoljno:
  jedan proces, a Turnstile i globalni dnevni limit mejlova pokrivaju restart):
  - registracija: 5/h po IP, 3/h po emailu;
  - „pošalji ponovo“: 60 s razmak, 5/dan po emailu;
  - unos koda: 5 pokušaja po tokenu (u bazi), 20/15 min po IP;
  - reset: 5/h po IP, 3/h po emailu;
  - **globalni limit slanja** (npr. 250/dan, ispod limita provajdera) iz `email_log` —
    da bot ne može da potroši kvotu i zablokira reset lozinki.
- **Lozinka:** min 10 znakova (postojeći `minPasswordLen`), max 72 bajta, odbija se
  ako je na listi čestih lozinki (SecLists 10k najčešćih, MIT, filtrirano na ≥10
  znakova + domaći dodaci: `lozinka123`, `sifra12345`, `dani90…`) ugrađenoj preko
  `go:embed`, ili ako sadrži deo emaila ispred `@` ili naziv firme. Jedna funkcija
  `auth.CheckNewPassword` za signup, reset, pozivnicu i promenu lozinke
  (zamenjuje `validateNewPassword`, `handlers_password.go:12-23`, i proveru u
  `validateNewUser`).
- **Disposable domeni:** opciono, lista `disposable-email-domains` (CC0) ugrađena;
  poruka „Koristite poslovnu ili ličnu adresu“.
- Svi javni POST-ovi iza `http.NewCrossOriginProtection` kao login (`server.go:53`).
- Audit: `RecordAudit` za `signup_verified`, `password_reset`, `invite_sent`, `invite_accepted`.

### 3.6 Trial

- `trial_until` = danas po Beogradu + 30 dana (izračunato u Go-u preko `internal/clock`,
  ne `date('now')` iz SQLite-a — `tenant.go:70` računa u UTC-u, pa signup u 00:30 dobija
  dan manje).
- **Limit 10 aktivnih vozača** dok je `plan = 'trial'`: provera u `store.CreateDriver` i
  `store.SetDriverActive(active=true)` u istoj transakciji. Pokriva web i oba puta kroz
  bot; bot već prikazuje `ValidationError` („Nije dodat: …“, `telegram/admin.go:130-132`).
  Poruka: „U probnom periodu možete imati do 10 aktivnih vozača. Za više nam pišite.“
- **Read-only posle isteka** (`plan='trial' && danas > trial_until`, ili `plan='paused'`):
  - `Tenant` dobija `Plan`, `TrialUntil` (`tenant.go:18-25`) i metodu
    `Writable(today) bool` — jedno pravilo za web i bot.
  - Web: middleware `requireWritable` u lancu na `server.go:108` posle `requireTerms`.
    GET prolazi; POST odbija sa flash-om i preusmerava na `/pretplata` (strana sa
    cenom, brojem vozača i kontaktom za fakturu). Dozvoljeno i u read-only:
    `/logout`, `/prihvatanje`, `/podesavanja/lozinka`, izvoz CSV (GET) i
    `/vozaci/{id}/obrisi-trajno` (prava lica ne smeju zavisiti od plaćanja).
  - Bot: provera u admin komandama koje pišu (`/dodaj`, `/ispravi`, dugmad) i u unosu
    vozača — **vidi otvoreno pitanje 1**.
  - Traka u `layout.html`: „Probni period: još N dana“ (poslednjih 7 dana žuta), posle
    isteka „Nalog je samo za čitanje — Pretplata“.
- **Podsetnici** (job runner, `jobs.processTenant`): 7 dana pre, 3 dana pre, na dan
  isteka i dan posle (read-only). Email svim aktivnim vlasnicima + Telegram vlasnicima
  sa `telegram_chat_id`. Dedup kroz `TryRecordAlert(tenant, 0, "trial_7", trial_until)`
  — ključ sa datumom, pa produženi trial podseća ponovo (isto kao `notifier.go:77`).
- **Superadmin:** prve komande koje pišu, uz postojeći audit:
  - `/produzi <id> <dana>` ili `/produzi <id> YYYY-MM-DD`;
  - `/placeno <id>` (plan paid) i `/pauza <id>`.
  Sve kroz `store.SetTenantBilling` (`store/superadmin.go:205`). Odgovor ponavlja firmu i
  novi rok, da se pogrešan id odmah vidi.

### 3.7 Izvor posete (UTM)

Sajt (`dani90.rs`) i app (`app.dani90.rs`) su različiti origin-i, pa:

1. `site/js/main.js` pri **prvoj** poseti sačuva u `localStorage` (`d90_src`):
   `utm_source/medium/campaign/content/term`, origin `document.referrer`, putanju
   ulaska, datum; `fbclid` samo kao `true/false` (sama vrednost je identifikator klika).
2. Svaki link ka `app.dani90.rs/registracija` dobija te parametre u query.
3. `GET /registracija` prihvata samo poznate ključeve, seče na 100 znakova, upisuje u
   hidden polja; reklama koja vodi direktno na app radi isto. Bez novih kolačića.
4. Pri potvrdi se upisuje na tenant; push `new_tenant` (`notifier.go:69`) dobija
   „izvor: facebook / cpc / kampanja“; `/firma <id>` ga prikazuje.
5. Digest dobija red „registracije: započeto N, potvrđeno M“ (iz `email_tokens`).

Politika privatnosti (`privatnost.html:45`) mora da navede `localStorage` za izvor posete
na sajtu i drugi kolačić (`d90_signup`) u app-u.

### 3.8 Pravno

`tenants.terms_version/terms_accepted_at/terms_accepted_by` već postoje (0022). Signup
upisuje verziju iz forme (`legal.TermsVersion`) i **vreme klika na „Registruj se“**
(iz payload-a), `accepted_by` = nov vlasnik. Pošto je verzija tekuća, `requireTerms`
(`handlers_legal.go:44-66`) ga ne šalje na `/prihvatanje`. Ako se `TermsVersion`
promeni između forme i potvrde, vlasnik prihvata ponovo — ispravno ponašanje.

Uz to: email provajder upisati u podobrađivače (`privatnost.html:55`,
`obrada_podataka.html:23`), Turnstile pod Cloudflare (već naveden). `[POPUNITI]`
podaci pružaoca (`legal.go:9-13`) moraju biti popunjeni pre otvaranja signupa — mejl
bez adrese pošiljaoca i uslovi bez pružaoca nisu za javnost.

### 3.9 Email

**Interfejs** (`internal/mail`):

```go
type Message struct {
    To, Subject, Text, HTML string
    Tag string // "signup", "reset", "invite", "trial_7" … za log i provajder
}
type Mailer interface { Send(ctx context.Context, m Message) error }
```

- `LogMailer` (dev, kad `SMTP_HOST` nije podešen): ispisuje primaoca, naslov i tekst
  sa linkom/kodom u log — `make run` radi bez naloga kod provajdera.
- `SMTPMailer` (net/smtp + STARTTLS, port 587): svi kandidati nude SMTP relej, pa je
  promena provajdera samo promena env-a (`SMTP_HOST/PORT/USER/PASS`, `MAIL_FROM`).
  Timeout 10 s; slanje u handleru (sinhrono), greška → „Slanje nije uspelo, pokušajte za
  minut“. Outbox sa ponavljanjem ne treba za ovaj obim.
- `email_log` (tag, tenant_id, primalac, vreme, status, greška; čuva se 90 dana) —
  za podršku („nije mi stigao mejl“), globalni dnevni limit i dedup podsetnika.
- `config.Load` odbija polovičnu SMTP konfiguraciju, kao što već radi za bot.

**Provajderi** (proveriti cene pri otvaranju naloga — stranice se menjaju):

| | Besplatno | Prvi plaćeni | Isporučivost | Podaci | Go |
|---|---|---|---|---|---|
| **Brevo** (FR) | 300/dan (~9 000/mes), deljeno sa marketingom; Brevo brending na besplatnom planu | Starter ≈ $9/mes | dobra, deljeni IP | **EU** (OVH Francuska/Nemačka, Google Cloud Belgija) | SMTP relej ili REST |
| **Resend** | 3 000/mes, **100/dan** | Pro $20/mes, 50 000 | dobra | slanje iz Irske moguće, ali podaci naloga i logovi **u SAD** | zvanični Go SDK, SMTP |
| **Postmark** | 100/mes (samo test) | Basic $15/mes, 10 000 | **najbolja** za transakcione | **SAD**, nema EU regiona (DPA + SCC) | SMTP, REST |
| **Amazon SES** | nema stalnog; AWS krediti za nove naloge | $0,10 / 1 000 | dobra, ali reputaciju vodiš sam | EU region (Frankfurt) | aws-sdk-go-v2 ili SMTP; sandbox (200/dan, samo verifikovani primaoci) dok se ne odobri produkcija |

**Preporuka: Brevo preko SMTP-a.** Obim je mali (desetine mejlova dnevno), podaci
ostaju u EU (lakše za politiku privatnosti i DPA prema firmama), besplatni dnevni
limit je 3× veći od Resend-ovog, a SMTP drži vrata otvorena za Postmark ako
isporučivost bude problem. Pre odluke proveriti: da li se Brevo brending dodaje i na
transakcione mejlove na besplatnom planu (ako da, Starter je ~$9/mes).

**DNS za dani90.rs (Cloudflare, svi zapisi „DNS only“, siva oblačić):**

| Zapis | Ime | Vrednost |
|---|---|---|
| TXT (verifikacija) | `dani90.rs` | `brevo-code:…` iz Brevo panela |
| CNAME (DKIM) | `brevo1._domainkey`, `brevo2._domainkey` | vrednosti iz Brevo panela |
| TXT (SPF) | `dani90.rs` | `v=spf1 include:spf.brevo.com ~all` — **jedan** SPF zapis; ako primanje pošte (npr. Cloudflare Email Routing, Google) već ima SPF, spojiti `include:` u isti zapis |
| TXT (DMARC) | `_dmarc` | početak `v=DMARC1; p=none; rua=mailto:dmarc@dani90.rs; adkim=r; aspf=r`, posle 2–4 nedelje čistih izveštaja `p=quarantine`, pa `p=reject` |
| MX | `dani90.rs` | za prijem odgovora i `rua` izveštaja (Email Routing ili postojeći sanduče) |

Pošiljalac `Dani90 <nalog@dani90.rs>`, `Reply-To: podrska@dani90.rs` (stvaran sanduče).
Provera pre puštanja: test mejl u Gmail i Outlook stiže u inbox, zaglavlja pokazuju
`spf=pass dkim=pass dmarc=pass` (AI-43).

### 3.10 Šabloni mejlova

`internal/mail/templates/*.txt` i `*.html` (html/template, jednostavna tabela, bez
slika i spoljnih fontova). Obraćanje sa „Vi“, kratko.

**Potvrda registracije** — naslov: `Vaš kod za Dani90: 482913`

```
Zdravo {{.Name}},

kod za potvrdu naloga firme {{.Firm}}:

    482913

Ili otvorite link: {{.URL}}

Kod i link važe 24 sata i mogu se iskoristiti jednom.
Niste se registrovali na Dani90? Ignorišite ovaj mejl — nalog se neće otvoriti.

Dani90 · app.dani90.rs
```

**Već imate nalog** — naslov: `Prijava na Dani90`

```
Neko je pokušao da otvori nalog sa ovom adresom, ali ona već postoji.
Prijava: {{.LoginURL}}
Zaboravili ste lozinku? {{.ResetURL}}
Ako to niste bili vi, ne morate ništa da radite.
```

**Reset lozinke** — naslov: `Nova lozinka za Dani90`

```
Zdravo {{.Name}},
za novu lozinku otvorite link (važi 1 sat, jednom): {{.URL}}
Posle promene bićete odjavljeni sa svih uređaja.
Niste tražili promenu? Ignorišite mejl; lozinka ostaje ista.
```

(+ kratko obaveštenje posle uspešne promene: „Lozinka je promenjena {{.When}}. Ako to
niste bili vi, odmah zatražite novu: {{.ResetURL}}“.)

**Pozivnica** — naslov: `{{.Inviter}} vas poziva u {{.Firm}} na Dani90`

```
Zdravo {{.Name}},
{{.Inviter}} vas je dodao kao {{.Role}} u firmi {{.Firm}}.
Izaberite lozinku i prijavite se: {{.URL}}
Link važi 7 dana. Dani90 prati dane vozača u Šengenu (pravilo 90/180).
```

**Podsetnik za trial** — naslov: `Probni period ističe za {{.Days}} dana`

```
Zdravo {{.Name}},
probni period za {{.Firm}} traje do {{.Until}}. Posle toga podaci ostaju,
ali nalog je samo za čitanje i upozorenja se ne šalju.
Imate {{.Drivers}} aktivnih vozača — {{.Monthly}} mesečno.
Za nastavak odgovorite na ovaj mejl ili otvorite {{.SubscriptionURL}}; šaljemo predračun.
```

### 3.11 Migracije

Sledeće po redu, postojeće se ne menjaju:

- `0023_email_tokens.sql` — `email_tokens` (3.2) i `email_log`.
- `0024_signup_source.sql` — na `tenants`: `contact_phone TEXT`, `src_utm_source`,
  `src_utm_medium`, `src_utm_campaign`, `src_utm_content`, `src_utm_term`,
  `src_referrer`, `src_landing`, `src_fbclid INTEGER` (0/1), `signup_at TEXT`
  (NULL = napravljen kroz seed).

Nijedna kolona na `users` nije potrebna: nalog nastaje tek kad je mejl potvrđen
(signup) ili kad pozvani postavi lozinku (pozivnica), pa je svaki nov nalog
verifikovan po konstrukciji.

---

## 4. Plan implementacije — 4 PR-a

Procene su za jednog developera koji zna kod; uključuju testove.

### PR 1 — Mailer, email tokeni, reset lozinke (≈ 3 dana) — urađeno, grana `signup-1-mailer-reset`

Odstupanja od plana: limit po emailu za reset broji se tek posle Turnstile
provere (inače neko bez rešene provere može satima da blokira tuđi reset);
Cloudflare-ove test tajne ne vraćaju `action`, pa se za njih ta provera
preskače; server van lokalnog razvoja bez SMTP-a ne ispisuje mejlove u log
(reset linkovi bi završili u journald-u), nego ih beleži kao neuspele.

- `internal/mail`: `Mailer`, `LogMailer`, `SMTPMailer`, šabloni (txt+html), `email_log`.
- `config`: `SMTP_*`, `MAIL_FROM`, `MAIL_DAILY_LIMIT`, `TURNSTILE_SITE_KEY/SECRET`;
  polovična konfiguracija = greška.
- Migracija `0023`; `store/email_token.go` (Create poništava stare, atomski Consume,
  DeleteExpired u `jobs.Cleanup`).
- `auth.CheckNewPassword` + ugrađena lista; zameniti proveru u promeni lozinke i
  „Dodaj korisnika“.
- `internal/web/turnstile.go` (siteverify, test ključevi u dev-u), CSP po ruti.
- Rute: `GET/POST /zaboravljena-lozinka`, `GET/POST /nova-lozinka?t=` ; link na `login.html`.
- Ops (paralelno): Brevo nalog, DNS zapisi, DMARC `p=none`, test u Gmail/Outlook.
- Testovi: reset uspeva i briše sesije; istekao / iskorišćen / tuđi purpose token;
  isti odgovor za postojeći, nepostojeći i deaktiviran nalog; rate limit po IP i
  emailu; globalni dnevni limit; Turnstile odbijen → 4xx bez slanja; česta lozinka
  odbijena; `LogMailer` hvata sadržaj u testu.

### PR 2 — Registracija i potvrda (≈ 4 dana)

- Rute (javne, iza CrossOriginProtection): `GET/POST /registracija`,
  `GET /registracija/proverite-mejl`, `POST /registracija/kod`,
  `POST /registracija/ponovo`, `GET/POST /potvrda`.
- Kolačić `d90_signup`, payload, pre-preuzimanje (3.3), mejl „već imate nalog“.
- `store.CreateTenantWithOwner` u jednoj transakciji: tenant (slug sa sufiksom `-2`,
  `-3` ako je zauzet), owner, terms, izvor, `trial_until` po Beogradu; email zauzet u
  međuvremenu → potvrda ne pravi duplikat, korisnik dobija „Nalog već postoji — prijavite se“.
- Migracija `0024`; UTM u `site/js/main.js` + CTA „Probajte besplatno 30 dana“ na
  sajtu (AI-49); `new_tenant` push sa izvorom; `/firma` prikazuje izvor.
- Izmena politike privatnosti (kolačić, localStorage, email provajder).
- Šablon `registracija.html` za telefon: polja ≥16px (iOS zum), `autocomplete`
  (`organization`, `name`, `email`, `new-password`, `tel`), greške pored polja.
- Testovi: ceo tok kodom i linkom; link u drugom pregledaču → nema sesije, traži
  lozinku; GET linka ne troši token; 6. pogrešan kod ubija token; istekao (25h) i
  iskorišćen token; isti odgovor i jedan mejl za nov / postojeći email; rate limit;
  bez čekboksa → greška; terms upisani i nema preusmerenja na `/prihvatanje`; UTM se
  čisti i seče; dve registracije istog emaila → važi samo poslednji kod;
  nova firma ne vidi podatke druge (proširiti `isolation_test.go`).

### PR 3 — Onboarding i trial (≈ 4–5 dana)

- `Tenant.Plan/TrialUntil/Writable`; middleware `requireWritable` + izuzeci; provera
  u botu (prema odluci na pitanje 1).
- Limit 10 aktivnih vozača u `CreateDriver`/`SetDriverActive` (trial).
- Traka triala u `layout.html`; strana `/pretplata` (cena × aktivni vozači, kontakt).
- `/dobrodosli` u 3 koraka nad postojećim delovima (forma vozača, link/QR, prvi
  boravak); checklist na pregledu ostaje.
- Podsetnici 7/3/0/−1 dana, email + Telegram vlasniku; dedup.
- Superadmin `/produzi`, `/placeno`, `/pauza` sa auditom; digest „započeto/potvrđeno“.
- Testovi: POST posle isteka → odbijen, GET radi, izuzeci rade (lozinka, trajno
  brisanje, izvoz); `paid` firma nikad read-only; granica dana po Beogradu (23:59 /
  00:01); 11. vozač odbijen kroz web, `/dodaj` i reaktivaciju; podsetnik ide jednom,
  a posle `/produzi` ponovo; `/produzi` od ne-admina se ne izvršava i upisuje se u audit.

### PR 4 — Pozivnice dispečera (≈ 2–3 dana)

- „Dodaj korisnika“ postaje „Pozovi“: ime, email, uloga → uvek „Pozivnica poslata“
  (SEC-08); lista „na čekanju“ sa „pošalji ponovo“ i „poništi“.
- `GET/POST /pozivnica?t=`: dispečer bira lozinku → `users` red, sesija.
- Pozvani email već postoji → poseban mejl pozvanom, vlasnik ne saznaje ništa.
- Ukloniti polje lozinke iz forme vlasnika.
- Testovi: prihvatanje pravi korisnika u **toj** firmi i sa tom ulogom; token jedne
  firme ne radi za drugu; poništena / istekla pozivnica; dispečer ne može da poziva
  (RequireRole); odgovor vlasniku je isti za slobodan i zauzet email; limit vozača i
  read-only važe i za pozvane.

**Ukupno ≈ 13–15 radnih dana** + ops (Brevo, DNS) ≈ ½ dana + pravni tekstovi.
Signup se otvara za javnost tek kad su sva četiri PR-a u produkciji i popunjeni
`[POPUNITI]` podaci pružaoca; PR 1 je koristan odmah (reset lozinke za postojeće firme).

---

## 5. Otvorena pitanja (za potvrdu)

1. **Bot posle isteka triala.** Predlog: web i admin komande bota su read-only, ali
   bot i dalje **prima unose vozača** 14 dana posle isteka (inače vozač koji javi
   granicu dobija grešku za stvar koju ne kontroliše, a firma gubi podatke koji je
   najviše vrede kad plati); upozorenja i digest prestaju odmah. Posle 14 dana plan
   prelazi u `paused`. Alternativa: sve read-only od prvog dana.
2. **Reset 1h** umesto 24h? (signup 24h, pozivnica 7 dana)
3. **Kod + link** u mejlu umesto samo linka (zbog FB in-app pregledača)?
4. **Brevo** kao provajder (EU podaci, SMTP)?
5. „Dodaj korisnika sa lozinkom“ **potpuno zameniti** pozivnicom u PR 4?
6. Šta posle `paused` (rok brisanja, AI-32) — van ovog plana, ali traka i mejl treba
   da kažu korisniku šta sledi.

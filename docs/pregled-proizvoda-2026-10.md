# dani90.rs — PO i tehnički pregled (14.10.2026)

## Executive summary

1. Jezgro proizvoda je solidno: kalkulacija 90/180 je čista i dobro testirana, tenant izolacija je dosledna i pokrivena testovima, a bot pokriva i vozača (dugmad) i dispečera (komande, brzi unos, CSV).
2. Pregled je našao tri stvarna baga u brojanju i sva tri su ispravljena: vozač koji se vrati u Šengen istog dana dobijao je grešku; Go i JS kalkulator su dodirujuće i preklopljene boravke brojali dvaput; Monako, San Marino i Vatikan nisu bili tretirani kao Šengen.
3. Lokacija je samo stub: postoje tipovi i tabela `events`, ali bot lokaciju ignoriše. Zato tačnost zavisi isključivo od toga da li vozač pritisne dugme, a dugmad su pogrešno nazvana („Izašao iz Srbije“, a znače „ušao u Šengen“).
4. Za produkciju najviše fale osnovne stvari: server umesto laptopa, backup baze, monitoring, pravna dokumenta, reset lozinke, export i audit sa starim i novim vrednostima.
5. EES je u punoj primeni od 10.04.2026, a EK ne menja pravilo 90/180 za vozače. Pritisak na firme je zato veći nego ikad, a direktni konkurenti su malobrojni. To je prilika, ali proizvod mora jasno da kaže da je interna evidencija, a ne zvanični podatak.

---

## 1. Inventar feature-a (iz koda)

| Feature | Status | Dokaz |
|---|---|---|
| Login email + lozinka | gotovo | `internal/web/handlers_auth.go`, `internal/auth/password.go` (bcrypt, dummy hash protiv timing napada) |
| Sesije u bazi, CSRF | gotovo | `internal/auth/session.go`, `internal/auth/csrf.go`, migracija `0008_sessions.sql` |
| Rate limit logina | delimično | `internal/auth/ratelimit.go`: 5 pokušaja / 15 min po IP+email, samo u memoriji |
| Reset ili promena lozinke, 2FA | nema | nema rute u `internal/web/server.go` |
| Login na klijentskom domenu | nema (namerno uklonjeno) | `0013_login_by_email.sql`: svi koriste `app.dani90.rs`, firmu određuje email |
| Multi-tenancy | gotovo | `tenant_id` u svim tabelama, sve `store` metode primaju `tenantID`; `internal/web/isolation_test.go`, `TestWebhook_AdminSeesOnlyHisFirm` |
| Tenant izolacija na nivou baze | nema | SQLite nema RLS, izolacija zavisi od disciplinovanog koda u `store` |
| Uloge owner / dispatcher | gotovo | `0001` CHECK; `auth.RequireRole("owner")` za podešavanja i korisnike (`server.go:63-68`); `telegram/roles.go` |
| Vozač kao korisnik | delimično | vozač nema web nalog, identitet mu je samo `drivers.telegram_chat_id` |
| Upravljanje korisnicima | delimično | postoji samo kreiranje (`handlers_users.go`), nema brisanja, deaktivacije ni promene uloge |
| Unos boravka na webu | gotovo | `handlers_driver.go`: dodavanje, izmena, brisanje, „vratio se“ |
| Unos boravka u botu, vozač | gotovo | `telegram/driver.go`, poništavanje 10 min (`undo.go`) |
| Unos boravka u botu, dispečer | gotovo | `quickentry.go` (tekst „Marko 22.04-29.04 p“), `/dodaj`, `/ispravi` |
| CSV uvoz (web + bot) | gotovo | `internal/importer/csv.go`, `handlers_import.go`, `admin.go:286`; importer nema testove |
| Validacija unosa | gotovo | `store/stay.go validateStay`: bez budućih datuma, bez preklapanja, izlaz ≥ ulaz; najviše jedan otvoren boravak (`0003` unique index) |
| Kalkulacija 90/180 | gotovo | `internal/calc/calc.go` + golden testovi `calc_test.go` |
| Planer ture („može li na turu“) | gotovo | `status/plan.go`, `/planer`, web „provera ture“ |
| Kalendar UI | gotovo | `internal/web/calendar.go`, `templates/vozac_detalj.html` (radni i privatni dani, van prozora, projekcija, oslobađanje) |
| Upozorenja dispečeru | gotovo | `internal/jobs/jobs.go`: crveno <15, narandžasto <30 (podesivo), prekoračenje, ≤3 dana do roka, dnevni pregled u 07h |
| Upozorenja vozaču | delimično | samo podsetnik „da li si još u EU“ posle 7 dana otvorenog boravka; nema poruke „ostalo ti je <10 dana“ |
| Kanal upozorenja | delimično | samo Telegram, u jedan chat po firmi; ako chat nije podešen, upozorenja se ne šalju |
| Izveštaji / export (CSV/PDF) | nema | nema rute ni koda |
| Dashboard / semafor | gotovo | `handlers_dashboard.go`, `pregled_filter.go`, `/pregled` u botu |
| Istorija izmena (audit) | delimično | `audit_log` beleži ko, šta i kad, ali `before_json/after_json` su svuda `nil` (14 poziva `RecordAudit`). Web prikaz ne postoji, bot `/ispravi` pokazuje poslednjih 5 |
| Lokacija iz Telegrama | stub | `types.go` ima `Location`, tabela `events` (`0004`) i `calc.IsSchengen` postoje, ali `bot.go:142` ignoriše `edited_message`, a `handleDriverMessage` čita samo tekst |
| Healthcheck | gotovo | `GET /healthz` u `cmd/server/main.go` |
| Deploy / backup | nema | `Makefile deploy`: „TODO (Faza 6)“; Litestream se pominje samo u README |
| CI | nema | nema `.github/` |
| Javni kalkulator na sajtu | gotovo | `site/kalkulator.html`, `site/js/calc90.js` (ručni prevod Go logike u JS) |
| Demo podaci | gotovo | `cmd/demo/main.go` |

### Provera ispravnosti 90/180

| Pravilo / slučaj | Ponašanje | Test |
|---|---|---|
| Dan ulaska i dan izlaska se broje | ✅ | `TestSameDayEntryExit`, `TestOpenStayCountsThroughToday` |
| Rolling prozor [d-179, d] | ✅ | `TestStaySpanningWindowBoundary`, novi `TestWindow_FirstAndLastDayOfWindow` |
| Tačno 90 je dozvoljeno, 91 je prekoračenje | ✅ | `TestExactly90IsOk`, `TestNinetyOneIsOverstay` |
| Prestupna godina | ✅ | `TestLeapYear` |
| Prelaz preko ponoći | ✅ računa se lokalni datum (Europe/Belgrade, `clock.Today`) | novi `TestMidnightCrossing_UsesLocalDate` |
| Privatni dani se broje | ✅ | novi `TestPrivateDaysCountTowardLimit` |
| Posle punih 90 dana, prvi slobodan dan | ✅ | novi `TestAfterFull90_FirstFreeDayIs180DaysAfterFirstEntry` |
| **Više prelaza istog dana / dodirujući boravci** | ❌ → ispravljeno: dan se brojao dvaput | novi `TestTouchingStays_SharedDayCountsOnce`, `TestSeveralCrossingsInOneDay_CountOnce` |
| **Preklopljeni boravci** | ❌ → ispravljeno. Store ih odbija, ali calc ih je sabirao | novi `TestOverlappingStays_CountUnionOfDays` |
| **Vozač izađe i vrati se u Šengen istog dana (Neum, kratko okretanje)** | ❌ → ispravljeno: bot je vraćao „Greška pri evidentiranju“. Sada nastavlja zatvoreni boravak, uz mogućnost poništavanja | novi `TestDriverExited_AgainOnTheDayHeReturned` |
| Tranzit kroz BiH/CG/MK/AL | ✅ samo kao uputstvo vozaču (`help.go`). Sistem ne zna rutu | — |
| Lista zemalja | ✅ 29 članica: RO i BG su unutra, CY, IE, GB i RS nisu | `schengen_test.go` |
| **Monako, San Marino, Vatikan** | ❌ → ispravljeno: dodati kao de facto Šengen. Andora namerno nije | novi `TestIsSchengen_CommonlyMisclassified` |
| Isti dan izlazak pa ulazak preko web forme | ⚠️ odbija se kao preklapanje; dispečer mora da spoji boravke ručno | neprovereno UX-om |

**Ispravke u ovom pregledu** (necommitovane): `internal/calc/calc.go` (UsedDays obeležava dane umesto da ih sabira), `site/js/calc90.js` (ista ispravka), `internal/calc/schengen.go` (mikro-države), `internal/telegram/driver.go` + `undo.go` (ponovni ulazak istog dana), novi `internal/calc/edge_test.go` i jedan test u `internal/telegram/bot_test.go`. `go test ./...` prolazi.

**Testovi koji još nedostaju**:
- `internal/importer` nema nijedan test: datumi dd.mm.gggg, BOM, `;` kao separator, prazan izlaz, nepoznat vozač.
- `internal/jobs` nema testove: deduplikacija, prag za upozorenje, dnevni pregled samo u 07h, podsetnik posle 7 dana.
- Paritet Go i JS kalkulatora: isti skup slučajeva kroz oba (npr. JSON fixture). Danas su to dve ručno održavane kopije.
- E2E flow bota: link → izašao → poništi → vratio se → moji dani, kroz `HandleWebhook` sa `fakeTelegram`.
- Ponovno slanje istog `update_id` (Telegram ponavlja kad odgovor kasni). Danas nema deduplikacije, pa bi duplikat „Izašao“ pokrenuo pitanje „Već imaš evidentiran izlazak…“.

---

## 2. Telegram bot

**Šta radi**
- Jedan zajednički bot za sve firme preko webhooka (`POST /telegram/webhook`). Tajna se proverava konstantnim vremenom, webhook se registruje sam pri startu (`main.go registerWebhook`). Polling ne postoji.
- **Povezivanje naloga:** jednokratni link `t.me/<bot>?start=<token>` koji važi 48h (`0005`, `handleStart`). Isti mehanizam služi i za admin naloge.
- **Vozač:** 4 dugmeta — „Izašao iz Srbije“, „Vratio se u Srbiju“, „Moji dani“ i „Pomoć“. „Poništi“ važi 10 min i preživljava restart jer je u `callback_data`.
- **Dispečer:** `/pregled`, `/vozac`, `/planer`, `/dodaj`, `/link`, `/ispravi`, `/sablon`, brzi tekstualni unos i CSV uvoz, sve uz potvrdu pre upisa.
- **Pozadinski poslovi:** tick na 5 min, deduplikacija preko `telegram_alert_log`.

**Slabosti iz koda**
- Natpisi dugmadi su „Izašao iz Srbije“, a uputstvo kaže „pritisni kad uđeš u Šengen“. Na ruti kroz BiH ili CG to nije isto i to je glavni izvor pogrešnog unosa. Predlog: „▶ Ušao u Šengen“ i „◀ Izašao iz Šengena“.
- Višekoračni tokovi (`pending` mapa) žive samo u memoriji, pa se gube pri restartu i ne rade sa više instanci.
- Nema deduplikacije po `update_id`. Obrada je sinhrona u HTTP handleru.
- Vozač ne dobija upozorenje kad mu ostane malo dana; dobija ga samo dispečer.

**Lokacija: nije implementirana.** `Message.Location` se parsira, ali niko ga ne čita. `EditedMessage` sa lokacijom se eksplicitno ignoriše (`bot.go:142`). Reverse geocodinga i geofence-a nema, a `IsSchengen` se koristi samo u testovima.

**Predlog implementacije (M):**
1. U `DriverKeyboard` dodati dugme `{"text":"📍 Pošalji lokaciju","request_location":true}`. `KeyboardButton` za to treba polje `RequestLocation`.
2. U `handleDriverMessage` obraditi `msg.Location`, a u `dispatch` i `EditedMessage.Location` (live lokacija).
3. Zemlju odrediti offline, point-in-polygon nad uprošćenim granicama država (Natural Earth 1:10m, ugrađen GeoJSON od nekoliko MB). Spoljni geocoding API ne treba, jer znači trošak, kašnjenje i slanje lokacije trećoj strani. U pojasu od ~2 km oko granice rezultat tretirati kao „neodređeno“.
4. Mapiranje: ako je zemlja u Šengenu a vozač nema otvoren boravak, bot predlaže „Ušao si u Šengen (HR)?“ sa dugmadima [Da] [Ne]. Obrnuto važi za izlazak. **U prvoj fazi bez automatskog upisa**: lokacija samo predlaže, vozač potvrđuje. Izvor upisa je `telegram_location` (dozvoljen je u CHECK-u).
5. Čuvanje: u `events` upisati samo `country_code`, datum i vreme. Koordinate posle obrade obrisati ili ih uopšte ne upisivati (minimizacija podataka po GDPR/ZZPL). `raw` ne puniti celom porukom.
6. Ako vozač ne potvrdi u roku od X sati, obavestiti dispečera.

**Da li je Telegram dovoljan kao jedini kanal?** Za MVP jeste, za tačnost nije.

| Rizik | Težina | Ublažavanje |
|---|---|---|
| Vozač zaboravi da pritisne | visoka | podsetnik već postoji (7 dana). Dodati: „u Šengenu si X dana, potvrdi“, dispečerov pregled „nepotvrđeno >24h“, poređenje sa planom tura |
| Nema pozadinskog praćenja | visoka | live lokaciju vozač pali ručno. Telegram od 2024. nudi i opciju „dok ne isključim“ (umesto max 8h), ali to zavisi od aplikacije u pozadini i baterije (neprovereno na terenu). Ne graditi na tome kao na jedinom izvoru |
| Vozač nema ili ne želi Telegram | srednja | dispečer unosi za njega (već postoji: brzi unos, CSV). Viber je u Srbiji rasprostranjeniji i ima bot API (neprovereno: troškovi i ograničenja Viber Business) |
| Privatnost lokacije (GDPR / ZZPL, čl. 6 i 12 ZZPL) | visoka | pristanak zaposlenog kod poslodavca je problematičan, pa je bolje osnov „legitimni interes + zakonska obaveza poštovanja 90/180“ uz DPIA. Čuvati samo zemlju i datum, bez koordinata. Lokacija mora biti opciona i vozač mora moći da je isključi |
| Telegram nedostupan ili bot blokiran | niska | web ostaje funkcionalan |

**Alternative, po vrednosti i naporu:**
1. Unos od strane dispečera, koji već postoji, uz bolji UX: „masovno: svi na turi X ušli 12.10“.
2. PWA za vozača sa web push notifikacijama (Android dobro, iOS od 16.4 samo kad je instalirana na home screen). Nema pozadinske lokacije, pa nije bolja od Telegrama za praćenje, samo za vozače bez Telegrama.
3. Integracija sa GPS/telematikom koju firme već imaju (Almaks, Webfleet…): vozilo ≠ vozač, ali kombinovano sa „ko vozi koje vozilo“ daje automatski predlog. Za kombije B kategorije bez tahografa GPS je često jedini automatski izvor (neprovereno koliko klijenata ga ima).
4. EES kao izvor: EES uredba predviđa web servis gde putnik proverava preostali dozvoljeni boravak (neprovereno: da li je javno dostupan i automatizovan; API za treća lica ne postoji). Realno: vozač ručno uporedi i „usaglasi sa EES“ jednim klikom.

---

## 3. Konkurencija

| Konkurent | Ciljna grupa | Ključni feature-i | Cena | Kanal | Šta oni imaju, a mi nemamo |
|---|---|---|---|---|---|
| 90dayschengen.com („onesy“) | firme i dispečeri + vozači, prisutan u Srbiji (srpski jezik) | svi vozači na jednom mestu, kalendar, menadžer/dispečer nalozi, vozač unosi sam | €3.99/vozač prema briefu (neprovereno, cena nije javno nađena) | web + Android app (iOS neprovereno) | native app, vozač ima sopstveni login, 8 jezika |
| Almaks GPS — modul Šengen 90/180 (gpsplus.rs) | prevoznici sa ≥10 vozila na Almaks GPS-u | automatsko brojanje dana iz GPS pozicija, prikaz flote, upozorenja | na upit | web (dodatak GPS platformi) | potpuno automatski unos. Ali broji **vozilo, ne vozača**, što je za nas argument |
| EK zvanični short-stay kalkulator (home-affairs.ec.europa.eu) | pojedinci | unos datuma, preostali dani, kontrola | besplatno | web | zvanični autoritet; nema flotu, istoriju ni upozorenja |
| Travel to Europe (Frontex, EES app) | putnici | EES informacije i priprema podataka pre granice | besplatno | iOS/Android | zvaničnost; prikaz preostalih dana neproveren |
| Schengen Simple / Schengen Calculator 90/180 i slične app | pojedinci, nomadi | planiranje ture, PDF izveštaj, Apple Watch, „EES passport control mode“ | besplatno do ~$3 | iOS | PDF izveštaj, offline, widgeti |
| 90 Days In Europe | pojedinci + „Professional“ za više putnika | više putnika, planer | $9.99/god, Professional $14.99/god | app | jeftin multi-traveler plan |
| Tahograf softver (Inelo Tachoscan, Webfleet, Tachomaster) | flote kamiona | radno vreme, tahograf | — | web | 90/180 brojanje nije nađeno (neprovereno). Ako ga dodaju, to je ozbiljna pretnja za segment kamiona, manje za kombije bez tahografa |

**EES — šta menja:**
- **Činjenice:** EES je počeo progresivno 12.10.2025, a u punoj primeni je od **10.04.2026**. EK je avgusta 2026. potvrdila da se pravilo 90/180 za profesionalne vozače u kratkom roku ne menja. Rešenja vidi u nacionalnim dugoročnim vizama i boravišnim dozvolama i u bilateralnim dogovorima. Prevoznici iz regiona su protestovali i blokirali granice u januaru i septembru 2026.
- **Prilika:** svaki dan prekoračenja se sad beleži automatski, pa deportacija i zabrana ulaska postaju realni. Firme koje su ranije „računale na pečat“ sada moraju da planiraju. Planer ture i upozorenja dispečeru su tačno ono što im treba.
- **Pretnja:** (a) ako EK ili države uvedu izuzeće ili vize za vozače, potreba opada za deo vozača; (b) ako EES web servis za proveru preostalih dana postane lako dostupan, pojedinačni kalkulator gubi vrednost. Vrednost za flotu (planiranje, upozorenja, više vozača) ostaje.
- **Za proizvod:** dodati polje „vozač ima D vizu / boravišnu dozvolu (zemlja, važi do)“ koje isključuje brojanje za tu zemlju ili period. Dodati i „usaglasi sa EES“ (ručna korekcija na zvanični broj uz zapis u auditu).

---

## 4. Gap analiza za produkciju

### Infrastruktura
| Prioritet | Stavka | Napomena |
|---|---|---|
| Must | Hosting umesto laptopa | README plan je dobar: Hetzner CX22 + Caddy + systemd. Alternativa: Fly.io sa volumenom. Cloudflare Tunnel može ostati ispred VPS-a |
| Must | Backup baze | Litestream → R2, uz test vraćanja. Danas `dani90.db` i `shengen.db` leže u radnom folderu bez ikakvog backupa |
| Must | Monitoring / alerting | uptime check na `/healthz` (UptimeRobot / Better Stack) + alarm na Telegram |
| Must | Alarm kada webhook ne prima update-e | `getWebhookInfo` (last_error_date) u jobs |
| Should | Strukturisani logovi | `log/slog` JSON, rotacija; danas `log.Printf` |
| Should | CI/CD | GitHub Actions: `go vet`, `go test`, build, deploy na tag (Makefile `deploy` je TODO) |
| Must | SSL / domen | Cloudflare (već je u planu); `app.dani90.rs` + sajt na Workers (`site/wrangler.jsonc`) |
| Could | Custom domen po klijentu | arhitektura je namerno napustila host po firmi (`0013`); ne vraćati bez jasnog zahteva |

### Bezbednost
| Prioritet | Stavka | Napomena |
|---|---|---|
| Must | Reset i promena lozinke | danas lozinku može promeniti samo `cmd/seed` |
| Must | Deaktivacija korisnika + gašenje njegovih sesija | nema |
| Must | Audit sa before/after | `RecordAudit(..., nil, nil)` svuda; za spor „ko je promenio datum“ to je ključno |
| Should | Rate limit van logina | webhook je zaštićen tajnom; web forme i uvoz nemaju limit |
| Should | Tajne | `.env` je van gita (OK); u produkciji systemd `EnvironmentFile` sa 0600; plan rotacije bot tokena |
| Should | Security headers na app-u | CSP, HSTS (sajt ima `_headers`, app neprovereno) |
| Could | 2FA za owner naloge | — |
| Must | Tenant izolacija | već dobra; zadržati `isolation_test.go` kao obavezan za svaku novu rutu |

### Pravno
| Prioritet | Stavka |
|---|---|
| Must | Politika privatnosti (ZZPL + GDPR, jer se obrađuju podaci o kretanju u EU): rukovalac = firma, obrađivač = dani90 |
| Must | Ugovor o obradi podataka (DPA) sa svakom firmom |
| Must | Uslovi korišćenja + disclaimer „nije pravni savet, zvanično stanje vodi EES“ (u aplikaciji tekst postoji, na sajtu i u ToS fali) |
| Must pre lokacije | Obaveštenje vozaču i opciona lokacija, DPIA, rok čuvanja (npr. 2 godine, pa brisanje) |
| Should | Brisanje ili izvoz podataka vozača na zahtev (prava lica) |

### Biznis
| Prioritet | Stavka |
|---|---|
| Must | Self-service onboarding firme (danas je to `make seed` na laptopu) ili barem admin stranica za kreiranje firme |
| Must | Support kanal: Telegram/Viber broj + email u footeru i u `/pomoc` |
| Must | Minimalno fakturisanje: broj aktivnih vozača po firmi po mesecu (upit nad `drivers.active`), PDF/Excel predračun, polje „plaćeno do“ na firmi i blokada upisa posle isteka + grace period |
| Should | Trial 14–30 dana (polje `trial_until`) |
| Should | Export CSV/PDF po vozaču (za kontrolu, ugovor ili spor) |
| Should | Onboarding vozača: masovno slanje linkova, status „povezan / nije“ na listi |
| Could | Engleski / ruski / ukrajinski interfejs za vozače iz drugih zemalja |

### Kvalitet
| Prioritet | Stavka |
|---|---|
| Must | Testovi za `importer` i `jobs` |
| Must | Paritet Go ↔ JS kalkulatora preko zajedničkih fixture-a |
| Should | E2E bot flow kroz webhook (+ duplikat `update_id`) |
| Should | Smoke test posle deploya (login + `/healthz` + `getWebhookInfo`) |

---

## Roadmap

### Now (pre prvog plaćenog klijenta)
| Stavka | Napor |
|---|---|
| Commit ispravki iz ovog pregleda | S |
| Preimenovati dugmad u „Ušao u Šengen“ / „Izašao iz Šengena“ | S |
| VPS + Caddy + systemd + Litestream backup + uptime alarm | M |
| Reset i promena lozinke, deaktivacija korisnika | M |
| Audit sa before/after JSON-om | S |
| Politika privatnosti, ToS, DPA šablon, disclaimer na sajtu | M (pravnik) |
| Admin komanda za kreiranje firme + „plaćeno do“ | S |
| Testovi za importer i jobs | S |

### Next (1–2 meseca)
| Stavka | Napor |
|---|---|
| Upozorenje vozaču (<10 dana, „ostalo X do roka“) | S |
| Export CSV/PDF po vozaču i za firmu | M |
| Lokacija: dugme `request_location` + offline geofence + potvrda (bez auto-upisa) | M |
| Dedup `update_id`, perzistentni `pending` u bazi | S |
| CI (GitHub Actions) + deploy skripta | S |
| Polje „D viza / boravišna dozvola“ po vozaču | M |
| Trial i jednostavni predračun | S |

### Later
| Stavka | Napor |
|---|---|
| Live lokacija kao automatski predlog ulaska i izlaska | M |
| Integracija sa GPS/telematikom (Almaks i sl.) | L |
| PWA za vozače bez Telegrama / Viber bot | L |
| „Usaglasi sa EES“ i praćenje promena pravila za vozače | M |
| Višejezični UI | M |
| Online naplata | M |

---

**Izvori:** [gpsplus.rs — Šengen 90/180](https://gpsplus.rs/en/vehicle-in-schengenvozilo-u-sengenu/), [onesy: 90 Day Schengen (Google Play)](https://play.google.com/store/apps/details?id=com.app90dayschengen&hl=en_US), [eucrim — EES fully operational 10.04.2026](https://eucrim.eu/news/entryexit-system-ees-fully-operational-since-10-april-2026/), [gov.pl — EES](https://www.gov.pl/web/usa-en/entryexit-system---full-implementation-from-april-10th-2026), [Vreme — EK: pravila ostaju ista](https://vreme.com/drustvo/evropska-komisija-prevoznici-sengen/), [nova.rs — moguće dugoročne vize](https://nova.rs/vesti/svet/ek-o-problemima-vozaca-sa-zapadnog-balkana-pravilo-90-dana-ostaje-moguce-dugorocne-vize-i-dozvole-boravka), [logistika.ba — odgovor EK](https://www.logistika.ba/en/evropska-komisija-o-90-180-nema-kratkorocnog-rjesenja-za-profesionalne-vozace-odgovornost-sada-prelazi-na-drzave/), [90 Days In Europe — poređenje aplikacija](https://90daysineurope.com/2026/03/27/best-schengen-calculator-apps-tools-compared-2026-find-your-perfect-90-180-day-tracker/), [EK short-stay kalkulator (Gulf News opis)](https://gulfnews.com/living-in-uae/visa-immigration/how-the-schengen-short-stay-calculator-helps-you-track-your-90-day-limit-1.500274450), [Travel to Europe FAQ](https://travel-europe.europa.eu/dam/jcr%3A1429f2b3-ac8e-4c6b-914c-2ebbdb063fa9/FAQ_app_en.pdf), [Schengen Simple (App Store)](https://apps.apple.com/app/id1637045933).

# Action plan — konsolidacija review-a
Datum: 2026-10-14 | Commit: 9c28a39

Izvori: [01-po.md](01-po.md) (47 nalaza), [02-ux.md](02-ux.md) (37), [03-qa.md](03-qa.md) (13), [04-security.md](04-security.md) (22), [05-legal.md](05-legal.md) (18) + [06-devops.md](06-devops.md) (26) = 163 nalaza → **60 action itema** (19 P0, 29 P1, 12 P2). Tabela za tracker: [00-action-items.csv](00-action-items.csv). Pravni nacrti: [legal-drafts/](legal-drafts/).

## Summary
Kalkulacija 90/180 je tačna: QA je proverio 420 scenarija kroz Go, JS i nezavisni orakl i 7 složenih scenarija u zvaničnom EU kalkulatoru, bez razlike. Rizik je u unosu: bot u toku „Da, ispravi“ ostavlja vozača bez otvorenog boravka (AI-02), a na EET granicama upisuje dan ranije (AI-03). Jedini ozbiljan bezbednosni propust je IDOR u potvrdi CSV uvoza, kojim firma A piše i blokira vozače firme B (AI-01); ostali security itemi su ojačavanje, ali su po pravilu P0. Pravno ništa ne postoji (politika, ToS, DPA, obaveštenje vozaču, brisanje), a infrastruktura je laptop bez backupa. Realno: P0 ≈ 5 nedelja, P1 još ≈ 9, otvoren signup ≈ 19. nedelja.

## Pravila prioriteta primenjena u konsolidaciji
- Greške u kalkulaciji i security propusti su P0. Zato je QA-02 podignut sa P1 na P0, a svi security propusti koji danas postoje u kodu su P0, i kad su ih reviewer-i ocenili P1/P2 (SEC-02…SEC-22).
- Zaštita signupa (AI-17) je P0, ali blokira samo otvaranje signupa, ne prvog plaćenog klijenta.
- Prikaz „Preko limita −X“ (AI-24) nije greška u broju preostalih dana nego u oznaci istorijskog prekoračenja, a `calc.Overstays` se ne koristi u produkciji, pa je ostao P1.
- Infra itemi AI-10, AI-13, AI-14, AI-18, AI-19, AI-32, AI-43 su potvrđeni u DevOps review-u ([06-devops.md](06-devops.md)); AI-53 je delimično osporen (deploy na tag iz GitHub-a ne treba, smoke i rollback su u AI-13). DevOps je dodao AI-57 do AI-60.
- Napor: S ≈ 2h, M ≈ 5h, L ≈ 12h (procena za planiranje sprintova).

## Spojeni duplikati (najvažniji)
| Tema | Izvorni ID-jevi | Item |
|---|---|---|
| Dugmad bota „Izašao iz Srbije“ | PO-11, UX-09, UX-23, UX-36, QA-13 | AI-21 |
| Potvrda brisanja (`hx-confirm` na običnoj formi) | UX-01, napomena u SEC | AI-20 |
| Dodir boravaka istog dana na webu/CSV-u | PO-15, QA-04 | AI-23 |
| Deaktivacija korisnika, sesije, lozinka | SEC-02, PO-17, PO-18, UX-18 | AI-06, AI-44 |
| Headeri, htmx/fontovi sa CDN-a | SEC-03, SEC-19, SEC-20, PO-22, LEG-07 | AI-08 |
| Dedup `update_id`, sinhrona obrada | SEC-10, PO-08, PO-09 | AI-09 |
| Audit before/after | PO-19, LEG-17 | AI-31 |
| Politika, ToS, identitet, placeholderi | LEG-01, LEG-10/11/12, PO-25, PO-27, UX-10 | AI-15, AI-30 |
| DPA, prenos van Srbije | LEG-02, LEG-08, LEG-13, PO-26 | AI-16 |
| Prava lica, izvoz | LEG-04, PO-29, PO-34 | AI-33 |
| Importer testovi i greške | QA-09, PO-43 | AI-26 |
| Paritet Go/JS | PO-45, QA-11 | AI-27 |
| Lokacija | PO-12, PO-28, LEG-06, QA-13, PO-16 | AI-46 |
| Signup zaštita | SEC-07, SEC-08, UX-32 | AI-17 |
| Upozorenje vozaču | PO-13, UX-24 | AI-50 |
| Fakturisanje | PO-30, PO-32, LEG-15 | AI-35 |
| Trial | UX-31, PO-33 | AI-47 |

## Konflikti i rešenja
| # | Konflikt | Rešenje |
|---|---|---|
| 1 | UX: signup sa 3 polja i jednim čekboksom. LEG: dokaz o prihvatanju DPA, obaveštenja, podaci za fakturu. | Zadržati 3 polja + jedan čekboks sa linkovima na Uslove, Politiku i DPA; u bazi pamtiti verzije, vreme i korisnika (AI-36). Podaci za fakturu (PIB, MB, adresa) traže se tek pri prvom predračunu (AI-35), ne na signupu. Obaveštenje vozaču ide kroz bot (AI-29), ne kroz signup. |
| 2 | UX: pauziran trial čuva podatke 12 meseci. LEG: brisanje po isteku ugovora + 30 dana, minimizacija. | Predlog: neplaćen nalog posle pauze briše se za 90 dana uz najavu 30 i 7 dana pre i ponuđen CSV izvoz; plaćeni klijent posle raskida 30 dana. Rok konačno potvrđuje pravnik i upisuje se u politiku i DPA (AI-32, AI-47). |
| 3 | PO: backup Litestream → Cloudflare R2. LEG: izbeći prenos u SAD. | Litestream na EU skladište (npr. Hetzner Object Storage); R2 samo ako pravnik potvrdi Cloudflare DPA kao meru po čl. 65 ZZPL (AI-14). Potvrđeno u DevOps review-u (OPS-04). |
| 4 | PO: lokacija je „Next“ (M). LEG: P0. | Feature ostaje P2 (AI-46). LEG-06 je uslov: lokacija se ne uključuje bez DPIA, LIA, opcionog uključivanja i bez čuvanja koordinata. Sadašnji rad nije blokiran. |
| 5 | PO-15 P1 (procena) vs QA-04 P2 za dodir istog dana na webu. | P1 (AI-23): dispečer mora da unese istoriju iz EES-a/pasoša pre prvog klijenta. |
| 6 | UX login B: prijava preko Telegrama. SEC-09: identitet je `chat.id`, grupe nisu blokirane. | Login B tek posle AI-05 (privatni chat, `from.id`) i uz serversku proveru Login Widget hash-a (AI-48). |
| 7 | UX: limiter za signup čuvati u SQLite. SEC-05: postojeći limiter je zaobilaziv. | Jedan novi limiter (AI-07) koji se koristi i za signup (AI-17). |

## Neslaganja oko činjenica (provereno u kodu)
| Tvrdnja | Ko | Provereno | Tačno je |
|---|---|---|---|
| „Tenant izolacija je dosledna“ | PO | `internal/web/handlers_import.go:117` šalje `driverID` iz forme u `CreateStay`; `internal/store/stay.go:175` ne proverava vlasništvo; indeks `idx_stays_one_open_per_driver ON stays(driver_id)` (migracije 0003, 0011) | SEC-01: izolacija ima rupu na potvrdi uvoza. Ostali upiti su filtrirani (SEC). |
| „Ispravke su necommitovane“ | PO | `git show 9c28a39` | Commit-ovane su u 9c28a39. |
| Brisanje i deaktivacija imaju potvrdu (`hx-confirm` u šablonu) | kod vs UX | `vozac_detalj.html:134`, `podesavanja_vozaci.html:33`: `method="post"` bez `hx-post` | UX-01: potvrda se ne pojavljuje. |
| Tok „Da, ispravi“ radi | PO (bot opis) vs QA | `internal/telegram/driver.go:230-238`: `UpdateStay` zatvara danas, pa `CreateStay` od danas pada na preklapanju | QA-01: vozač ostaje bez otvorenog boravka. |
| Lista Šengen zemalja | PO, QA | `internal/calc/schengen.go` | Obe se slažu: 29 članica, RO i BG unutra, CY i IE van; mikro-države de facto. |

## Action itemi
| ID | naslov | opis | izvori | tip | kategorija | prioritet | napor | zavisnosti | kriterijum prihvatanja |
|---|---|---|---|---|---|---|---|---|---|
| AI-01 | CSV uvoz: upis boravka vozaču druge firme (IDOR) | POST /import/potvrdi veruje row_driver_id iz forme; CreateStay ne proverava da vozač pripada firmi. Firma A upisuje otvoren boravak vozaču firme B i blokira joj unos (500, bot ne radi). Panic kad su nizovi u formi različite dužine. | SEC-01, SEC-15, PO-24 | ispravka | security | P0 | S | - | Potvrda uvoza ponovo parsira fajl na serveru ili proverava GetDriver(tenant, driver); indeks jednog otvorenog boravka i FK su na (tenant_id, driver_id); novi test u isolation_test.go pada na starom kodu i prolazi na novom. |
| AI-02 | Bot: „Da, ispravi“ ostavlja vozača bez otvorenog boravka | Ponovljen „Izašao“ + „Da, ispravi“ zatvori stari boravak danas, a novi se odbije kao preklapanje; vozač dobija „Greška.“ i od sutra mu se dani ne broje. | QA-01 | ispravka | bug | P0 | S | - | Tok pomera ulaz otvorenog boravka (ili pita datum) u jednoj transakciji; test TestQA_ReopenConfirm prolazi sa InSchengen=true. |
| AI-03 | Datum prelaza po beogradskom vremenu na EET granicama | clock.Today() je uvek Europe/Belgrade; na granicama BG/RO/GR između 00 i 01h lokalno upisuje se prethodni dan, pa izlazak broji dan manje od EES-a. QA ga je ocenio P1; podignuto na P0 po pravilu da je svaka pogrešna kalkulacija P0. | QA-02 | ispravka | bug | P0 | S | - | Pritisak dugmeta između 23:00 i 01:30 nudi izbor datuma (danas/juče/sutra) ili se čuva vreme i zemlja; test sa ubrizganim satom. |
| AI-04 | Go toolchain na ≥1.26.9 + govulncheck | govulncheck: 18 dostižnih ranjivosti u stdlib (net/http, html/template, crypto/tls) u go1.26.5. | SEC-04 | ispravka | security | P0 | S | - | go.mod i toolchain ≥1.26.9; govulncheck ./... bez dostižnih nalaza; komanda u Makefile. |
| AI-05 | Telegram: samo privatni chat, from.id, jednokratni tokeni | /start u grupi daje admin/vozač prava celoj grupi (identitet je chat.id); link token se pod konkurencijom troši više puta; stari tokeni se ne poništavaju; deaktiviran vozač i dalje piše u bot i drži chat ID. | SEC-09, SEC-11, SEC-12 | ispravka | security | P0 | S | - | Bot odbija chat.type != private; callback proverava From.ID; ConsumeLinkToken je atomski UPDATE ... WHERE used_at IS NULL; novi token poništava stare; deaktivacija briše telegram_chat_id; testovi za sva četiri slučaja. |
| AI-06 | Upravljanje korisnicima: deaktivacija, promena lozinke, opoziv sesija | Dispečer koji ode zadržava pristup na webu (14 dana + ponovna prijava) i u botu; nema promene lozinke ni uloge. | SEC-02, PO-18, PO-17 (deo), UX-18 (deo) | feature | security | P0 | M | - | Vlasnik deaktivira korisnika i menja ulogu; korisnik menja svoju lozinku; deaktivacija i promena lozinke brišu sve sesije i Telegram vezu; test da deaktiviran korisnik dobija 401/redirect i da bot odbija komande. |
| AI-07 | Rate limit logina i poverenje u proxy zaglavlja | Ključ IP\|email dozvoljava password spraying; mapa raste bez granice; Allow/RecordFailure nisu atomski; CF-Connecting-IP se veruje i iza Caddy-ja, pa se limit zaobilazi. | SEC-05, SEC-06, PO-20 (deo) | ispravka | security | P0 | S | AI-13 (režim proxy-ja) | Nezavisni limiti po IP i po nalogu, cap mape + čišćenje, atomsko brojanje; TRUSTED_PROXY=cloudflare\|caddy\|none; testovi za spraying i lažni CF-Connecting-IP. |
| AI-08 | Security headeri; htmx i fontovi lokalno | App ne šalje CSP, frame-ancestors, HSTS, nosniff, Referrer-Policy (clickjacking na forme bez potvrde). htmx sa unpkg bez SRI, Google Fonts šalju IP korisnika u SAD. Sajt nema HSTS. | SEC-03, SEC-19, SEC-20, PO-22, LEG-07 | ispravka | security | P0 | S | - | Middleware postavlja svih 5 zaglavlja (test); htmx i fontovi iz internal/web/static; nijedan zahtev ka trećim domenima pri učitavanju app-a; HSTS na sajtu. |
| AI-09 | Telegram webhook: dedup update_id i asinhrona obrada | Spor odgovor izaziva retry i duplu obradu istog update-a; obrada je sinhrona u HTTP handleru. | SEC-10, PO-08, PO-09 | ispravka | security | P0 | S | - | Duplikat update_id se preskače (test); handler odgovara 200 pre obrade. |
| AI-10 | Limiti tela i polja, timeout-i servera, bind adresa | Uvoz bez MaxBytesReader; ime/napomena bez limita dužine; http.Server bez timeout-a (slowloris); sluša na :8080 na svim interfejsima. | SEC-13, SEC-14, OPS-25 | ispravka | security | P0 | S | - | Globalni MaxBytesReader (1MB, uvoz 2MB) i limit redova CSV-a; ime ≤100, napomena ≤500; Read/Write/Idle/ReadHeader timeout; podrazumevani bind 127.0.0.1. DevOps: potvrđeno u kodu (7a1e40a); bind 127.0.0.1 odgovara Caddy-ju i tunelu na istom hostu (OPS-25: tunel na 127.0.0.1, ne localhost). |
| AI-11 | Sesije: heš tokena, idle istek, čišćenje | Tokeni sesija u bazi u čistom obliku; samo apsolutni istek 14 dana; DeleteExpiredSessions se nigde ne poziva. | SEC-17, LEG-03 (deo) | ispravka | security | P0 | S | - | U bazi SHA-256 tokena; idle istek 24h; dnevni job briše istekle sesije i tokene (test). |
| AI-12 | Sitni security propusti | Flash poruke iz URL-a (?err= phishing); login bez CSRF; cmd/seed prima lozinku kao argument; baza 0644 u repou. | SEC-16, SEC-21, SEC-22, SEC-18 | ispravka | security | P0 | S | - | Flash iz sesije ili fiksne liste kodova; login proverava Origin/double-submit; seed čita lozinku sa stdin-a; baza 0600 i van repoa u produkciji. |
| AI-13 | Hosting na VPS-u umesto laptopa | App i baza rade na laptopu kroz go run iza tunela, bez nadzora procesa. Cilj: Hetzner CX22 + Caddy (DNS-only zapis) + systemd; cloudflared na VPS-u samo kao prelaz. time/tzdata je već uključen. Deploy je TODO, bez smoke testa i rollback-a. | PO-01, PO-21, QA-10, SEC-18 (deo), OPS-01, OPS-05, OPS-06, OPS-11, OPS-12, OPS-13, OPS-14 | feature | infra | P0 | M | AI-04 | App radi na VPS-u kao systemd servis (korisnik dani90, StateDirectory 0700, UMask 0077, hardening iz 06-devops); TRUSTED_PROXY=caddy uz DNS-only zapis, nikad Caddy iza Cloudflare proxy-ja; tajne u EnvironmentFile root 0600; Cloud Firewall 80/443 + filtriran 22, SSH samo ključem, unattended-upgrades; deploy.sh pravi .backup pre restarta, radi smoke (/healthz, /login, getWebhookInfo) i vraća prethodni binary ako smoke padne; restart preživljava. |
| AI-14 | Backup baze sa testom vraćanja | Nema backupa. Odluka: Litestream → Hetzner Object Storage (EU), ne R2, zbog LEG-08; R2 samo ako pravnik potvrdi Cloudflare DPA. | PO-02, LEG-08 (deo), OPS-04, OPS-21 | feature | infra | P0 | M | AI-13 | Litestream kao poseban servis (korisnik dani90), retencija 7 dana, dnevni snapshot; vraćanje izvedeno na čistoj mašini po proceduri iz 06-devops (integrity_check, broj redova, /healthz), RPO i RTO zapisani; test se ponavlja mesečno; Hetzner Backups uključen. |
| AI-15 | Politika privatnosti, uslovi korišćenja i identitet pružaoca | Nema politike ni ToS-a nigde; footer sajta ima placeholdere (naziv, PIB, kontakt); kontakt forma (Web3Forms) bez linka na politiku. Nacrti postoje u legal-drafts/. | LEG-01, LEG-11, LEG-12, LEG-10 (deo), PO-25, PO-27, UX-10 | feature | legal | P0 | M | Pravnik | Pravnik je pregledao nacrte; /privatnost i /uslovi objavljeni; linkovi na sajtu, loginu i u footeru app-a; footer ima naziv, adresu, MB, PIB, email, telefon; kontakt forma ima napomenu i link. |
| AI-16 | DPA, evidencija obrade, procedura za povredu podataka | Nema ugovora o obradi sa firmama (čl. 45 ZZPL), evidencije radnji obrade, procedure za povredu ni tabele podobrađivača i prenosa van Srbije (Telegram, Cloudflare, Web3Forms, hosting). | LEG-02, LEG-08, LEG-13, PO-26 | feature | legal | P0 | M | AI-15, Pravnik | Potpisan ili elektronski prihvaćen DPA sa svakom firmom (sa listom podobrađivača); evidencija i procedura od 1 strane u repou; pravnik potvrdio osnov za prenos u SAD/UAE. |
| AI-17 | Zaštita signupa od zloupotrebe | Signup još ne postoji. Pre otvaranja: verifikacija emaila, Turnstile, rate limit u SQLite (3 reg./h po IP, 5 kodova/h po emailu), ista poruka bez obzira da li email postoji (danas „Dodaj korisnika“ otkriva tuđ nalog), odbijanje disposable domena, limit 10 vozača u trialu, obaveštenje vlasniku za svaku registraciju. Blokira samo otvaranje signupa. | SEC-07, SEC-08, UX-32 | feature | security | P0 | M | AI-43 | Svi navedeni mehanizmi rade i imaju testove; „Dodaj korisnika“ više ne otkriva da email postoji u drugoj firmi. |
| AI-18 | Monitoring i alarm za webhook | Nema uptime provere ni alarma kad Telegram webhook ne prima update-e; klijent nema getWebhookInfo; alarm samo kroz Telegram ne stiže kad Telegram padne. | PO-03, PO-04, OPS-07, OPS-08 | feature | infra | P1 | S | AI-13 | Timer na VPS-u na 5 min: /healthz preko javnog URL-a, disk < 80%, getWebhookInfo (url, last_error_date, pending_update_count) → ping na healthchecks.io; izostanak pinga ili /fail šalje email i Telegram; test: zaustavljen servis daje alarm za ≤ 15 min. Job u app-u opciono kasnije. |
| AI-19 | SSL i domen za app i sajt | app.dani90.rs i sajt na Cloudflare Workers treba finalizovati. HSTS već šalju i app i sajt; domen ne šalje email, a verovatno nema SPF/DMARC (neprovereno), pa je moguć lažni pošiljalac. | PO-05, OPS-16, OPS-20 | feature | infra | P1 | S | AI-13 | HTTPS na oba domena (Caddy za app, „Always Use HTTPS“ za sajt), HSTS zadržan bez preload; CAA zapis; dok nema emaila SPF „v=spf1 -all“, DMARC p=reject i null MX. |
| AI-20 | Potvrda pre brisanja boravka i deaktivacije vozača | hx-confirm ne radi na običnim formama, pa se brisanje i deaktivacija izvršavaju bez pitanja (provereno u vozac_detalj.html:134, podesavanja_vozaci.html:33). | UX-01, SEC (napomena) | ispravka | bug | P1 | S | - | Klik traži potvrdu (ili nudi „Poništi“ 10 s); provereno u pregledaču. |
| AI-21 | Dugmad i tekstovi bota u terminima Šengena | „Izašao iz Srbije“ znači „ušao u Šengen“, pa vozač na ruti kroz BiH/CG klikne pogrešno; podsetnik pita „još u EU“; mešanje ti/Vi. | PO-11, UX-09, UX-23, UX-36, QA-13 (deo) | ispravka | ux | P1 | S | - | Dugmad „▶ Ušao u Šengen“ / „◀ Izašao iz Šengena“; „još u Šengenu“; vozaču „ti“, dispečeru „Vi“; isti termini na webu i u pomoći. |
| AI-22 | „Vratio se“ bez prethodnog „Izašao“ | Ako vozač zaboravi „Izašao“, „Vratio se“ samo odgovori poruku; dispečer ništa ne saznaje i boravak se ne broji. | QA-03 | ispravka | bug | P1 | S | - | Bot pita datum ulaska i obaveštava dispečera; test TestQA_ReturnWithoutExit. |
| AI-23 | Izlaz i ulaz istog dana na webu, u CSV-u i brzom unosu | Dodirujući boravci se odbijaju kao preklapanje (bot ih već spaja), pa istorija iz EES-a sa okretanjem na granici ne može da se unese. | PO-15, QA-04 | ispravka | bug | P1 | S | - | Dodir istog dana se prihvata ili spaja uz poruku na sva tri ulaza; testovi. |
| AI-24 | Prikaz prekoračenja („Preko limita −X“) | OverBy = UsedDays − 90 nije broj dana prekoračenja i zbunjuje kad je vozač u Srbiji; calc.Overstays broji i dane van Šengena (ne koristi se u produkciji). Preostali dani su tačni. | QA-05, QA-06 | ispravka | bug | P1 | S | - | Razdvojeno „trenutno prekoračenje“ i „istorijsko: N dana, od–do“; Overstays ispravljen ili obrisan; scenariji E1 i E3 se poklapaju sa EU kalkulatorom. |
| AI-25 | Validacija unosa dispečera | Brzi unos bez godine tiho prebacuje budući datum u prošlu godinu; /planer prihvata polazak u prošlosti; /ispravi „Poništi povratak“ bez upozorenja vraća sve dane do danas. | QA-07, QA-08, QA-12 | ispravka | bug | P1 | S | - | Budući datum bez godine se odbija ili jasno upozorava; planer odbija prošlost kao web; poništavanje povratka pokazuje koliko dana dodaje i traži potvrdu; testovi. |
| AI-26 | CSV importer: separator, zaglavlje, godina, testovi | Excel sa srpskim podešavanjima čuva sa `;`; fajl bez zaglavlja gubi prvi red; 01.09.26 se ne prihvata; importer nema testove. | QA-09, PO-43 | ispravka | qa | P1 | S | - | Automatsko prepoznavanje `,`/`;`, zaglavlje preskočeno samo ako nije datum, godina sa 2 cifre; unit testovi za BOM, separatore, prazan izlaz, nepoznatog vozača. |
| AI-27 | Testovi za jobs, E2E bot i paritet Go/JS | jobs nema testove; nema E2E toka bota; Go i JS kalkulator su ručne kopije (danas 420/420 isto). | PO-44, PO-45, PO-46, QA-11 | feature | qa | P1 | S | - | Testovi za dedup, pragove, 07h pregled, podsetnik; E2E link→izašao→poništi→vratio se; zajednički testdata/parity.json kroz Go i node u make test. |
| AI-28 | Status rečju, kontrast, HITNO link, terminologija | Status je samo boja i broj; narandžasti badge 2,8:1; link u HITNO baneru 1,1:1; „žuto“ vs „narandžasto“; KPI kartice ne filtriraju. | UX-03, UX-04, UX-05, UX-12, UX-13 | ispravka | ux | P1 | S | - | Badge = ikona + reč + broj (OK / Pažnja / Kritično / Preko limita), svi parovi ≥4,5:1, isti nazivi u filteru, botu i upozorenjima; KPI kartice filtriraju. |
| AI-29 | Obaveštenje vozaču pri povezivanju sa botom | Bot šalje samo „Povezani ste“; vozač ne zna ko vidi podatke (uključujući privatna putovanja i napomene) ni svoja prava. | LEG-05, LEG-16 | feature | legal | P1 | S | AI-15 | Pre „Povezani ste“ bot šalje obaveštenje iz legal-drafts/obavestenje-vozacu-telegram.md sa imenom firme i linkom; polje napomene ima uputstvo. |
| AI-30 | Disclaimer u botu, app-u i kalkulatoru | Rečenica postoji u footeru, ali bot piše „do kada smeš da ostaneš“ bez napomene; nema ugovornog ograničenja odgovornosti (dolazi sa ToS). | LEG-10, PO-27 (deo) | feature | legal | P1 | S | AI-15 | Tekst iz legal-drafts/disclaimer-kalkulacija.md u „Moji dani“, na kalkulatoru i u app-u. |
| AI-31 | Audit sa before/after i prikazom | RecordAudit se svuda zove sa nil, nil; nema web prikaza; bez toga nema dokaza ko je promenio datum. | PO-19, LEG-17 | feature | feature | P1 | S | - | Svaka izmena boravka i vozača upisuje JSON pre/posle; istorija izmena vidljiva na stranici vozača. |
| AI-32 | Rokovi čuvanja i automatsko brisanje | Ništa se ne briše: vozači, firme, audit log, tokeni, alert log rastu zauvek; journald rok nije podešen. | LEG-03, LEG-18, OPS-10 | feature | legal | P1 | M | AI-16, AI-11 | Rokovi upisani u DPA i politiku; dnevni job briše po rokovima; journald MaxRetentionSec=30day i SystemMaxUse=500M; Caddy bez access loga; rok čuvanja backupa (7 dana) upisan u politiku. |
| AI-33 | Prava lica: izvoz, trajno brisanje, odjava od bota | Nema izvoza po vozaču/firmi, trajnog brisanja vozača, odvezivanja od bota ni izmene imena vozača. | LEG-04, PO-29, PO-34 | feature | legal | P1 | M | - | CSV izvoz po vozaču i za firmu; „Obriši vozača trajno“ (boravci, veza); bot /odjava; izmena imena i registracije; procedura za zahteve na emailu. |
| AI-34 | Pravno mišljenje: GDPR čl. 3(2)(b) i predstavnik u EU | Nije jasno da li GDPR važi i da li treba predstavnik po čl. 27. | LEG-09 | feature | legal | P1 | S | Pravnik | Pisano mišljenje pravnika; ako je odgovor „da“, ugovoren predstavnik i naveden u politici. |
| AI-35 | Kreiranje firme, podaci za fakturu i obračun | Nova firma = make seed na laptopu; tenant nema PIB, MB, adresu, email za fakture ni „plaćeno do“; nema broja aktivnih vozača po mesecu ni predračuna. | PO-30, PO-32, LEG-15 | feature | feature | P1 | M | Knjigovođa | Admin stranica/komanda kreira firmu sa podacima za fakturu; mesečni broj aktivnih vozača; predračun (PDF/Excel); paid_until + grace period. |
| AI-36 | Prihvatanje uslova i DPA se pamti | Nema mesta za verziju i vreme prihvatanja uslova/DPA ni ko je prihvatio. | LEG-14 | feature | legal | P1 | S | AI-16 | Tenant ima terms_version, dpa_version, accepted_at, accepted_by_user_id; promena verzije traži ponovno prihvatanje. |
| AI-37 | Support kanal | Nema kontakta u footeru ni u /pomoc. | PO-31 | feature | feature | P1 | S | - | Telegram/Viber broj i email u footeru sajta, app-a i u /pomoc. |
| AI-38 | Pregled i stranica vozača na mobilnom | Na 390px vide se 2 od 7 kolona (preostali dani van ekrana); zaglavlje 150px; stranica vozača šira od ekrana; tabela na desktopu izlazi iz kontejnera. | UX-02, UX-22, UX-14, UX-11 | ispravka | ux | P1 | M | AI-28 | Ispod 700px kartice sa preostalim danima i statusom; mobilni meni; bez horizontalnog skrola na 390px (screenshot). |
| AI-39 | Hero preostalih dana na stranici vozača | Najvažniji broj je badge od 13px; redosled stavlja proveru ture iznad unosa; crveno „Obriši“ dominira. | UX-07, UX-33, UX-34 | ispravka | ux | P1 | M | AI-28 | Hero sa brojem, statusom i rokom; redosled hero → akcije → kalendar → istorija → tura; „Obriši“ sekundarno. |
| AI-40 | Prazna stanja sa prvim koracima | Nova firma vidi četiri nule; vozač bez boravaka vidi prazan kalendar. | UX-06, UX-17 | feature | ux | P1 | M | - | Kontrolna lista prvih koraka na Pregledu; poruka i dugmad za vozača bez boravaka. |
| AI-41 | Vozači: status Telegram veze i QR | Nema kolone „Telegram povezan“; na mobilnom akcije van ekrana; nema masovnog slanja linkova. | UX-08, PO-35 | feature | ux | P1 | M | AI-05 | Kolona ✓/✗ povezan; link i QR odmah posle dodavanja; masovno slanje linkova. |
| AI-42 | Stranica „Upozorenja“ i tiho neslanje | Bez podešenog chata upozorenja ne idu nikome; stranica meša tri teme i traži ručni chat ID. | UX-19, PO-14 | ispravka | ux | P1 | M | - | Baner na Pregledu dok upozorenja nemaju primaoca; koraci „Poveži svoj Telegram“ i „grupa“ bez ručnog ID-a; pragovi odvojeno. |
| AI-43 | Slanje emaila | App ne šalje email; preduslov za reset lozinke, pozivnice, verifikaciju i signup. | UX-29, OPS-17 | feature | infra | P1 | M | AI-13 | EU transakcioni servis (Brevo, Mailjet ili Scaleway TEM) sa poddomena mail.dani90.rs; SPF include, DKIM 2048, DMARC p=quarantine sa rua, posle 4 nedelje p=reject; test poruka stiže u inbox Gmail i Outlook (ne spam). |
| AI-44 | Reset lozinke i pozivnice emailom | Lozinku menja samo cmd/seed; vlasnik smišlja tuđu lozinku. | PO-17, UX-18, SEC-02 (deo) | feature | security | P1 | M | AI-43, AI-06 | Reset sa jednokratnim tokenom kratkog trajanja; pozivnica u kojoj korisnik bira lozinku; reset briše sve sesije. |
| AI-45 | Samostalna registracija i wizard | Nema ekrana za nekoga bez naloga. Tok: registracija (firma, email, lozinka, jedan čekboks) → kod 6 cifara → wizard 4 koraka → rezultat < 3 min. | UX-30 | feature | feature | P2 | L | AI-43, AI-36, AI-17, AI-15 | Nova firma dođe od sajta do prvog broja preostalih dana za < 3 min bez pomoći (test sa 3 prava korisnika). |
| AI-46 | Lokacija iz Telegrama (uz pravne preduslove) | Lokacija je stub. Ne uključivati bez DPIA, procene legitimnog interesa, opcionog uključivanja i bez čuvanja lat/lng/raw (LEG-06 je P0 za uključivanje lokacije, ne za sadašnji rad). | PO-12, PO-28, LEG-06, QA-13 (deo), PO-16 | feature | feature | P2 | M | AI-15, AI-16, AI-21, AI-29, Pravnik (DPIA) | DPIA urađena; dugme request_location; offline određivanje zemlje; samo predlog koji vozač potvrđuje; čuva se samo zemlja i datum. |
| AI-47 | Trial, read-only režim i Pretplata | Trial 30 dana bez kartice, podsetnici dan 23/28/30, read-only 31–44, pauza od 45, /pretplata sa predračunom. | UX-31, PO-33 | feature | feature | P2 | L | AI-45, AI-43, AI-35 | Ceo tok traje kako je opisano; podsetnici stižu emailom i Telegramom; rok brisanja pauziranih naloga usklađen sa AI-32. |
| AI-48 | Login redizajn (varijanta A pa B) | Nema logoa, autocomplete-a, „zaboravljena lozinka“ ni registracije; varijanta B dodaje prijavu preko Telegrama (Login Widget, provera hash-a). | UX-25, UX-26 | feature | ux | P2 | M | AI-44, AI-05 | A: logo, rečenica, autocomplete, greška pored polja. B: Telegram Login Widget sa serverskom proverom hash-a. |
| AI-49 | Sajt: CTA za trial i cena | CTA je samo „Zakažite demo“, cena samo u FAQ. | UX-27 | feature | ux | P2 | S | AI-45, AI-47 | Dugme „Probajte besplatno 30 dana“ i cena na sajtu; login preuzima brend. |
| AI-50 | Upozorenje vozaču na pragu | Upozorenja dobija samo dispečer. | PO-13, UX-24 | feature | feature | P2 | S | - | Vozač dobija „Ostalo ti je N dana. Moraš da izađeš najkasnije …“ na pragu. |
| AI-51 | Pristupačnost i sitni UX | Labele bez for/id; fokus samo browser default i ivica 1,3:1; nema loading stanja; 404/500 goli tekst na engleskom; uvoz bez šablona; Pomoć daleko od kolona; tekst 14–15px. | UX-15, UX-16, UX-20, UX-21, UX-28, UX-35, UX-37 | ispravka | ux | P2 | M | - | Sve labele povezane; :focus-visible i ivica ≥3:1; dugme „Čuvam…“; brendirane 404/500; šablon CSV; „?“ ikone; tekst 16px. |
| AI-52 | Perzistentni pending tokovi bota | Višekoračni tokovi su u memoriji i gube se na restartu. | PO-10 | ispravka | bug | P2 | S | - | pending u SQLite; tok preživi restart (test). |
| AI-53 | Strukturisani logovi, CI/CD i smoke test | log.Printf bez nivoa (vreme se duplira u journald-u); nema GitHub Actions. Deploy, smoke i rollback su prebačeni u AI-13. | PO-06, PO-07, PO-47, OPS-18, OPS-19 | feature | infra | P2 | S | AI-13 | slog bez duplog vremena, nivo ERROR za greške, chat ID se ne loguje; CI na push i PR: vet, test -race, govulncheck, linux build; na tag release artefakt; deploy ostaje ručni (deploy.sh), bez SSH ključa u GitHub-u. |
| AI-54 | 2FA za vlasnike | Nema drugog faktora za owner naloge. | PO-23 | feature | security | P2 | M | AI-06 | TOTP za owner naloge, kodovi za oporavak. |
| AI-55 | D viza / boravišna dozvola po vozaču | Vozač sa nacionalnom vizom ili dozvolom se broji kao da je nema. | PO-37 | feature | feature | P2 | M | - | Polje (zemlja, važi do) isključuje taj period iz brojanja; testovi u calc. |
| AI-56 | Backlog proizvoda | „Usaglasi sa EES“, masovni unos dispečera, višejezični UI, GPS/telematika, PWA/Viber, online naplata. | PO-36, PO-38, PO-39, PO-40, PO-41, PO-42 | feature | feature | P2 | L | - | Svaka stavka dobija sopstveni item kad uđe u plan. |
| AI-57 | Produkcijski TRUSTED_PROXY i provera pri startu | U produkcijskom .env nema TRUSTED_PROXY, pa iza tunela svi posetioci dele jedan IP limit: 20 pogrešnih prijava za 15 min blokira prijavu svim firmama. | OPS-02 | ispravka | security | P0 | S | - | .env ima TRUSTED_PROXY=cloudflare (na VPS-u caddy); app odbija start ili loguje upozorenje kad je PUBLIC_BASE_URL postavljen, a TRUSTED_PROXY=none; test u internal/config. |
| AI-58 | Kopija baze pre migracija 0014–0017 | Produkcijska baza je na migraciji 0013; sledeći start main-a prepisuje tabelu stays i preimenuje kolone token, a backup ne postoji. | OPS-03 | ispravka | infra | P0 | S | - | sqlite3 .backup pre prvog starta, kopija šifrovana i van laptopa; posle starta integrity_check ok i foreign_key_check prazan; korisnici obavešteni da se ponovo prijave. |
| AI-59 | Runbook za jednu osobu | Nema zapisanih koraka za restart, vraćanje backupa, rotaciju tokena i ključeva, pad Telegrama i pad VPS-a. | OPS-15, OPS-12 | feature | infra | P1 | S | AI-13, AI-14 | docs/runbook.md po skici iz 06-devops; vraćanje backupa i rotacija bot tokena izvedeni jednom po zapisanim koracima. |
| AI-60 | Upozorenje se gubi kad slanje ne uspe | jobs upisuje dedup red pre slanja; ako Telegram vrati grešku, isto upozorenje se ne šalje ponovo do sutra. | OPS-09 | ispravka | bug | P1 | S | - | Na grešku slanja dedup red se briše ili se upisuje tek posle uspeha; test sa lažnim Telegramom (500) pokazuje ponovno slanje u sledećem tick-u. |

## Sprintovi (1 nedelja ≈ 10h, jedna osoba)
Ukupno: P0 ≈ 52h, P1 ≈ 84h, P2 ≈ 69h. Pravnik i knjigovođa rade paralelno; nacrte im poslati u sprintu 3.

| Sprint | Itemi | Sati | Cilj |
|---|---|---|---|
| 1 | AI-01, AI-02, AI-03, AI-04, AI-05 | 10 | Zatvoriti P0 u podacima i izolaciji |
| 2 | AI-07, AI-08, AI-09, AI-10, AI-11 | 10 | Security ojačavanje |
| 3 | AI-06, AI-12, AI-20 | 9 | Korisnici, sitni propusti, potvrda brisanja; poslati `legal-drafts/` pravniku |
| 4 | AI-13, AI-14 | 10 | VPS + backup (gasi se laptop) |
| 5 | AI-15, AI-16 | 10 | Objaviti pravna dokumenta posle pregleda pravnika → **svi P0 osim AI-17 gotovi** |
| 6 | AI-18, AI-19, AI-21, AI-22, AI-23 | 10 | Monitoring, domen, bot tekstovi, unos |
| 7 | AI-24, AI-25, AI-26, AI-27, AI-28 | 10 | Validacija, importer, testovi, status rečju |
| 8 | AI-29, AI-30, AI-31, AI-36, AI-37 | 10 | Obaveštenje vozaču, disclaimer, audit, support |
| 9 | AI-33, AI-35 | 10 | Prava lica, fakturisanje (knjigovođa potvrdio polja) |
| 10 | AI-32, AI-34 | 7 | Rokovi čuvanja; pravno mišljenje o GDPR-u → **minimum za prvog plaćenog klijenta** (UX P1 tada ide kao pilot uz dogovor) |
| 11 | AI-38, AI-39 | 10 | Mobilni Pregled i stranica vozača |
| 12 | AI-40, AI-41 | 10 | Prazna stanja, status Telegram veze |
| 13 | AI-42, AI-43 | 10 | Upozorenja, slanje emaila |
| 14 | AI-44 | 5 | Reset lozinke, pozivnice; rezerva 5h → **svi P1 gotovi** |
| 15 | AI-17, AI-48 (varijanta A) | 10 | Zaštita signupa, login A |
| 16–17 | AI-45, AI-49 | 14 | Signup + wizard, CTA na sajtu; test sa 3 prava korisnika |
| 18–19 | AI-47 | 12 | Trial, read-only, Pretplata → **otvoren signup** |
| 20+ | AI-46, AI-50…AI-56, AI-48 (B) | — | P2 po potrebi |

Rizik: plan nema rezervu za bagove sa terena ni za čekanje na pravnika. Ako pravnik kasni, AI-15 i AI-16 se pomeraju, a sprint 5 se popunjava itemima iz sprinta 6.

## Definition of Done za produkciju (pre otvorenog signupa)
**Podaci i kalkulacija**
- [ ] AI-01, AI-02, AI-03 zatvoreni i pokriveni testovima; `go test ./...` prolazi.
- [ ] Paritet Go/JS se proverava automatski (AI-27); 7 EU scenarija iz 03-qa.md su u testovima.
- [ ] Unos istog dana, „Vratio se“ bez „Izašao“ i validacija dispečera rade (AI-22, AI-23, AI-25).

**Bezbednost**
- [ ] Svi P0 security itemi zatvoreni (AI-01, AI-04…AI-12, AI-17).
- [ ] govulncheck bez dostižnih nalaza; isolation_test pokriva svaku rutu koja prima ID.
- [ ] Deaktivacija korisnika, promena i reset lozinke rade (AI-06, AI-44).
- [ ] Signup ima verifikaciju emaila, Turnstile, rate limit i ne otkriva postojeće emailove (AI-17).

**Infra** (potvrđeno u [06-devops.md](06-devops.md))
- [ ] TRUSTED_PROXY postavljen u produkciji (AI-57); kopija baze pre migracija 0014–0017 (AI-58).
- [ ] App na VPS-u, ne na laptopu; baza van repoa, 0600 (AI-13).
- [ ] Backup u EU skladište i jednom urađeno vraćanje (AI-14).
- [ ] Uptime alarm i alarm za webhook (AI-18); HTTPS + HSTS (AI-08, AI-19).
- [ ] Email sa SPF/DKIM radi (AI-43); CI pokreće testove i govulncheck (AI-53, preporuka).

**Pravno**
- [ ] Politika, Uslovi i DPA pregledani od pravnika i objavljeni; linkovi na sajtu, loginu, u app-u i u signupu (AI-15, AI-16).
- [ ] Prihvatanje se pamti sa verzijom (AI-36); footer ima naziv, MB, PIB i kontakt (AI-15).
- [ ] Vozač dobija obaveštenje u botu (AI-29); disclaimer u botu i app-u (AI-30).
- [ ] Rokovi čuvanja definisani i automatsko brisanje radi (AI-32); izvoz i brisanje vozača postoje (AI-33).
- [ ] Pisano mišljenje o GDPR-u i predstavniku u EU (AI-34).
- [ ] Lokacija je isključena dok ne postoji DPIA (AI-46).

**Proizvod**
- [ ] Predračun i „plaćeno do“ rade; knjigovođa potvrdio sadržaj fakture (AI-35).
- [ ] Trial, read-only i Pretplata rade (AI-47); support kanal objavljen (AI-37).
- [ ] Bar 3 firme su prošle signup do prvog broja za < 3 min bez pomoći (AI-45).
- [ ] UX P1 itemi zatvoreni (AI-20, AI-21, AI-28, AI-38…AI-42).

## Šta mora da pregleda čovek
| Ko | Šta | Itemi |
|---|---|---|
| Pravnik (zaštita podataka) | Svih 5 nacrta u `legal-drafts/`; uloge rukovalac/obrađivač; pravni osnov i DPIA za lokaciju; GDPR čl. 3(2)(b) i čl. 27; prenos u SAD/UAE (da li je SAD još na listi Vlade, Cloudflare DPA kao mera po čl. 65); evidencija po čl. 47 st. 7; rokovi čuvanja (konflikt 2); domašaj ograničenja odgovornosti (ZOO); obavezni podaci po Zakonu o elektronskoj trgovini | AI-15, AI-16, AI-30, AI-32, AI-34, AI-46 |
| Knjigovođa | Elementi fakture/predračuna; PDV status i paušal; obaveza e-fakture preko SEF-a za B2B; poziv na broj; kurs ako je cena u evrima | AI-35, AI-47 |
| Pravi korisnici (dispečeri, vozači) | Nazivi dugmadi bota na stvarnoj ruti (BiH/CG); mobilni Pregled; signup wizard < 3 min; trajanje triala (30 dana) i cena €2/vozač; Telegram vs Viber; tekst obaveštenja vozaču | AI-21, AI-38, AI-45, AI-47, AI-29 |
| DevOps review | Urađen ([06-devops.md](06-devops.md)). Ostaje provera uživo: Cloudflare zona (DNS, SPF/DMARC/MX, SSL mode), cene Hetznera, Litestream sintaksa za instaliranu verziju | AI-13, AI-14, AI-18, AI-19, AI-43, AI-57, AI-58 |

## Neprovereno
- Sve „Neprovereno“ stavke iz izvornih izveštaja ostaju otvorene (videti 01–05). Najvažnije: ponoć/DST na pravom satu (sat se ne može ubrizgati), Telegram uživo, `internal/jobs` nije pokretan, Cloudflare podešavanja, da li demo preko tunela sadrži stvarne podatke klijenata, pravni status SAD na listi Vlade.
- Procena sati (S/M/L → 2/5/12h) je gruba i nije proverena na istoriji rada.
- Da li je minimum posle sprinta 10 prihvatljiv za prvog plaćenog klijenta bez UX P1 stavki: odluka vlasnika.

## Dodatne napomene
- Konsolidacija je proverila u kodu sva tri P0 iz QA i SEC (AI-01, AI-02) i tvrdnju o `hx-confirm` (AI-20). Ostali nalazi su preuzeti iz izveštaja uz dokaz koji tamo stoji.
- CSV koristi zarez kao separator i UTF-8; kolona `izvori` sadrži originalne ID-jeve za praćenje nazad do izveštaja.

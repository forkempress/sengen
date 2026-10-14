# Pravni review (ZZPL/GDPR)
Datum: 2026-10-14 | Commit: 9c28a39

> Ovo nije pravni savet. Pregled je napravljen iz koda i javnih izvora; sve označeno sa „pravnik“ mora da potvrdi advokat za zaštitu podataka, a stavke o fakturi knjigovođa.

## Summary
Aplikacija obrađuje malo podataka (ime vozača, oznaka vozila, Telegram chat ID, datumi boravka u Šengenu, email/ime korisnika) i tehnički je pristojno zaštićena (bcrypt, tenant izolacija, CSRF), ali pravno nije spremna za produkciju: nema politike privatnosti, uslova korišćenja ni ugovora o obradi (DPA), a vozač pri povezivanju sa botom ne dobija nikakvo obaveštenje. Ne postoji nikakvo brisanje ni rok čuvanja: vozači, korisnici, firme, audit log i tokeni se samo deaktiviraju ili gomilaju, a nema ni izvoza podataka. Aplikacija (ne sajt) učitava Google Fonts i htmx sa unpkg, čime IP adrese korisnika idu trećim stranama u SAD bez osnova i obaveštenja. Praćenje lokacije (planirano) menja profil rizika: pre uključivanja treba DPIA, procena legitimnog interesa, opciono uključivanje za vozača i pravno mišljenje da li GDPR važi po čl. 3(2)(b) (praćenje ponašanja u EU) i da li treba predstavnik u EU. Draftovi dokumenata su u `docs/reviews/legal-drafts/` i svi su NACRTI za pravnika.

## Nalazi
| ID | Naslov | Opis | Dokaz (fajl:linija / URL / screenshot) | Kategorija | Ozbiljnost | Predlog | Napor |
|---|---|---|---|---|---|---|---|
| LEG-01 | Nema politike privatnosti ni uslova korišćenja | Ni sajt, ni app, ni login nemaju link na politiku privatnosti ili uslove. Dani90 je rukovalac bar za naloge kupaca, kontakt formu i marketing, pa mora da pruži informacije iz čl. 23 ZZPL. UX plan za signup (čekboks „Prihvatam uslove i politiku“) nema na šta da se pozove. | `site/index.html:365-378` (footer bez linkova), `internal/web/templates/login.html`, `internal/web/templates/layout.html:39-41`; `docs/reviews/raw/ux.md:256` | legal | P0 | Objaviti `/privatnost` i `/uslovi` na sajtu (nacrti: `legal-drafts/politika-privatnosti.md`, `uslovi-koriscenja.md`), link u footeru sajta, na loginu i u footeru app-a; pravnik pregleda. | M |
| LEG-02 | Nema ugovora o obradi (DPA) sa firmama | Za podatke vozača firma je rukovalac, Dani90 obrađivač. Čl. 45 st. 3 ZZPL traži ugovor u pismenom (i elektronskom) obliku sa obaveznim elementima iz st. 4. Ne postoji ni šablon ni mehanizam prihvatanja. | nema fajla; `docs/reviews/raw/po.md:171` | legal | P0 | DPA kao prilog uslova, prihvata se klikom pri registraciji/prvoj fakturi, uz čuvanje verzije i vremena prihvatanja (vidi LEG-14). Nacrt: `legal-drafts/ugovor-o-obradi-dpa.md`. | M |
| LEG-03 | Nema brisanja ni roka čuvanja podataka | Vozač se samo deaktivira (`active=0`), podaci ostaju zauvek. Nema brisanja korisnika ni firme. `audit_log`, `telegram_link_tokens`, `telegram_alert_log` i `sessions` rastu neograničeno; `DeleteExpiredSessions` postoji ali se nigde ne poziva. Kršenje načela ograničenja čuvanja (čl. 5 ZZPL) i nemogućnost ispunjenja čl. 45 st. 4 tač. 7 (brisanje po prestanku ugovora). | `internal/store/driver.go:103`, `internal/web/server.go:58-59`, `internal/store/session.go:57` (bez poziva), `internal/jobs/jobs.go` (nema čišćenja), migracije 0005/0006/0010 | legal | P1 | Definisati rokove (predlog u Dodatnim napomenama), dodati: trajno brisanje vozača (sa boravcima i Telegram vezom), brisanje firme po isteku ugovora + 30 dana, dnevni job koji briše istekle sesije/tokene i stare zapise alert loga, rok za audit log. Rokove upisati u DPA i politiku. | M |
| LEG-04 | Prava lica nisu podržana u app-u | Nema izvoza podataka vozača/firme (prenosivost, čl. 36; pristup, čl. 26), nema brisanja vozača (čl. 30), vozač ne može da se odveže od bota (nema `telegram_chat_id = NULL` putanje), korisnik ne može da obriše nalog. Ispravka postoji samo za boravke; ime i oznaku vozila nije moguće izmeniti u UI-ju. Firma kao rukovalac zavisi od obrađivača za ispunjenje zahteva (čl. 45 st. 4 tač. 5). | `grep` za export/izvoz/unlink: nema pogodaka; ispravka: `internal/web/handlers_driver.go:200-232`, `internal/web/server.go:52-54` | feature | P1 | Minimalno: CSV izvoz po vozaču i za celu firmu, „Obriši vozača trajno“, bot komanda `/odjava` koja briše chat ID, ručna procedura za zahteve na [EMAIL ZA PRIVATNOST] dok se ne napravi UI. | M |
| LEG-05 | Vozač ne dobija obaveštenje pri povezivanju sa botom | Posle `/start` bot šalje samo „Zdravo, ime! Povezani ste.“ i uputstvo. Informacije iz čl. 23 ZZPL (ko je rukovalac, svrha, rok, prava) duguje firma, ali je bot jedini kanal ka vozaču. | `internal/telegram/bot.go:265` | legal | P1 | Pre poruke „Povezani ste“ poslati kratko obaveštenje + link (nacrt: `legal-drafts/obavestenje-vozacu-telegram.md`). Naziv firme je dostupan (`t.Name`). | S |
| LEG-06 | Lokacija: pravni preduslovi pre uključivanja | Lokacija je stub (tipovi, tabela `events` sa `lat`, `lng`, `raw`), planirano je slanje sa Telegrama. Praćenje lokacije zaposlenog je obrada visokog rizika: osnov ne treba da bude pristanak (neravnopravan odnos), već legitimni interes firme, uz test srazmernosti, DPIA (čl. 54 ZZPL), opciono uključivanje za vozača i minimizaciju (čuvati samo zemlju i datum, bez koordinata i bez `raw`). Takođe utiče na primenu GDPR (LEG-09). | `internal/db/migrations/0004_events.sql:7-10`, `internal/telegram/types.go:40-43`, `internal/telegram/bot.go:142-143`, `docs/reviews/raw/po.md:96-110` | legal | P0 | Blokira samo uključivanje lokacije, ne trenutni rad. Pre lansiranja: DPIA (šablon kod pravnika), LIA, ne upisivati `lat/lng/raw` (ili brisati odmah po određivanju zemlje), lokacija samo predlaže unos koji vozač potvrđuje, vozač može da isključi, ažurirati obaveštenje vozaču i DPA. | M |
| LEG-07 | Aplikacija učitava Google Fonts i htmx sa unpkg | Svaki prikaz stranice app-a šalje IP adresu i User-Agent korisnika Google-u i unpkg/Cloudflare CDN-u (SAD) bez obaveštenja i osnova; sajt je ovo već rešio (fontovi lokalno, CSP). Uz to skripta sa CDN-a bez SRI je bezbednosni rizik (CDN može da izmeni JS koji vidi sve podatke). | `internal/web/templates/layout.html:8-11`; poređenje: `site/_headers:2-7` | security | P1 | Fontove i htmx servirati iz `internal/web/static` (fontovi već postoje u `site/fonts/`), dodati CSP kao na sajtu. | S |
| LEG-08 | Prenos podataka van Srbije bez dokumentovanog osnova | Telegram (sve poruke vozača i upozorenja), Cloudflare (tunel/proxy terminira TLS i vidi sav saobraćaj app-a; Pages za sajt), budući Hetzner (EU), budući R2 backup, web3forms (kontakt forma). Za države članice Konvencije 108 (npr. Nemačka, Finska) primeren nivo se pretpostavlja (čl. 64 st. 2 ZZPL); za SAD i UAE treba mera iz čl. 65 (npr. standardne ugovorne klauzule Poverenika). | `README.md` (tunel, Hetzner, R2), `internal/telegram/client.go`, `site/index.html:309` | legal | P1 | Tabela podobrađivača u DPA (već u nacrtu), za Cloudflare zaključiti/prihvatiti DPA i proveriti da li pokriva SCC prihvatljive po ZZPL (pravnik), Litestream backup na Hetzner Object Storage (EU) umesto R2 ako se želi izbeći prenos u SAD, Telegram navesti kao nužni kanal (vidi Dodatne napomene). | M |
| LEG-09 | Primena GDPR i predstavnik u EU nisu razjašnjeni | Dani90 i firme nemaju sedište u EU. GDPR može da važi po čl. 3(2)(b) jer se beleži ponašanje (boravak, kasnije lokacija) lica dok su u EU. Ako važi, čl. 27 traži predstavnika u EU osim za povremenu obradu niskog rizika (čl. 27(2)); sistematsko praćenje teško da je „povremeno“. Nije provereno da li ovo važi i za evidenciju koju unosi firma bez lokacije. | GDPR čl. 3(2)(b), čl. 27 | legal | P1 | Pisano pravno mišljenje pre prvog plaćenog klijenta; ako je odgovor „da“, ugovoriti predstavnika u EU (komercijalne usluge postoje) i to navesti u politici. Do tada pisati dokumente tako da zadovolje i ZZPL i GDPR (nacrti to već rade). | S |
| LEG-10 | Ograničenje odgovornosti za grešku u kalkulaciji postoji samo kao napomena | Rečenica „Evidencija služi za internu kontrolu. Zvanično stanje vodi EES…“ stoji u footeru app-a, sajta, na pomoći i kalkulatoru, ali nema ugovornog ograničenja odgovornosti (uslovi ne postoje), a bot vozaču piše „do kada smeš da ostaneš“ bez ikakve napomene. Posledica greške (README: deportacija, zadržavanje vozila) je ozbiljna. | `internal/web/templates/layout.html:40`, `internal/web/templates/pomoc.html:115`, `site/index.html:259,378`, `site/kalkulator.html:165,224`, `internal/telegram/help.go:15` | legal | P1 | Uslovi korišćenja sa ograničenjem odgovornosti (nacrt), duži disclaimer na kalkulatoru i u app-u, jedna rečenica u odgovoru „Moji dani“ u botu (nacrt: `legal-drafts/disclaimer-kalkulacija.md`). Pravnik: domašaj ograničenja prema Zakonu o obligacionim odnosima (namera i krajnja nepažnja se ne mogu isključiti). | S |
| LEG-11 | Kontakt forma: nema linka na politiku, treća strana nije navedena | Forma šalje ime, firmu, telefon, email i poruku na web3forms (spoljni servis). Napomena „Podatke koristimo samo…“ je dobra, ali nema rukovaoca, roka čuvanja ni linka na politiku. Ključ je još placeholder. | `site/index.html:309-351` | legal | P1 | Dopuniti napomenu: „Rukovalac je [NAZIV]. Forma ide preko servisa Web3Forms. Više u Politici privatnosti.“ + link. Rok čuvanja upita: npr. 12 meseci. | S |
| LEG-12 | Identitet pružaoca usluge nije objavljen | Footer sajta ima placeholder za kontakt, naziv i PIB. Kupac i lice čiji se podaci obrađuju ne znaju ko je rukovalac/obrađivač. Zakon o elektronskoj trgovini propisuje obavezne podatke o pružaocu usluge (tačan član: pravnik). | `site/index.html:305,372-375` | legal | P1 | Upisati: naziv preduzetnika i radnje, adresu, MB, PIB, email, telefon u footer i u politiku. | S |
| LEG-13 | Nema evidencije radnji obrade i procedure za povredu podataka | Obrađivač vodi evidenciju po čl. 47 st. 4 (izuzetak za manje od 250 zaposlenih iz st. 7 ima uslove koje sistematska obrada verovatno ne ispunjava; pravnik). Čl. 52 st. 3: obrađivač bez odlaganja obaveštava rukovaoca o povredi. Nema dokumenta, kontakt liste firmi ni logova pristupa. | nema fajla | legal | P1 | Jednostrana evidencija (tabela iz Dodatnih napomena je dobra osnova) i procedura od 1 strane: kako se otkriva, ko se obaveštava, rok (u DPA predloženo 48 h), šablon emaila. | S |
| LEG-14 | Signup: prihvatanje uslova mora da se pamti | Planirani self-signup (UX) ima čekboks, ali u bazi ne postoji mesto za verziju i vreme prihvatanja uslova/DPA, niti ko je prihvatio u ime firme. Bez toga nema dokaza o zaključenom DPA. | `internal/db/migrations/0001_tenants_users.sql` (nema polja), `docs/reviews/raw/ux.md:224,256` | feature | P1 | Uz signup dodati `terms_version`, `dpa_version`, `accepted_at`, `accepted_by_user_id` na tenant; pri promeni verzije tražiti ponovno prihvatanje. | S |
| LEG-15 | Podaci za fakturu i SEF nisu predviđeni | Tenant ima samo `name` i `slug`; nema PIB, MB, adrese, emaila za fakture, „plaćeno do“. Za B2B fakturu u Srbiji ovo je neophodno; da li preduzetnik mora da izdaje e-fakturu preko SEF-a zavisi od statusa (proveriti sa knjigovođom). | `internal/db/migrations/0001_tenants_users.sql:1-7` | feature | P1 | Polja na tenantu + ručno vođenje dok nema UI; vidi „Minimum za fakturu“ u napomenama. | S |
| LEG-16 | Privatna putovanja i slobodna napomena vidljivi poslodavcu | Tip boravka `private` i polje `note` (200 znakova, slobodan tekst) vidljivi su vlasniku i dispečerima. Za pravilo 90/180 potreban je samo datum; razlog putovanja i slobodne beleške nisu nužni (minimizacija, čl. 5 ZZPL; čl. 91 – zaštita dostojanstva u radnom odnosu). | `internal/db/migrations/0003_stays.sql:7-9`, `internal/web/templates/vozac_detalj.html:91,94,112-114` | legal | P2 | U obaveštenju vozaču jasno reći da firma vidi i privatne ulaske; razmotriti da tip „privatno“ bude opcion; uz polje napomene uputstvo „ne upisujte zdravstvene ili lične razloge“. | S |
| LEG-17 | Audit log ne beleži stare i nove vrednosti | `before_json`/`after_json` su uvek `nil`, pa se ne može dokazati ko je i šta promenio (integritet, odgovornost iz čl. 5 st. 2 ZZPL; spor sa firmom oko pogrešnog broja dana). | `internal/web/handlers_driver.go:141,196,232,252`, `internal/telegram/driver.go:60,72,106` | qa | P2 | Puniti before/after za stay i driver; rok čuvanja audit loga uskladiti sa LEG-03. | S |
| LEG-18 | Logovi servera: sadržaj i rok čuvanja nisu definisani | Logovi sadrže Telegram chat ID i ID-jeve vozača/korisnika (malo, ali lični podatak). Rok čuvanja zavisi od systemd/journald na serveru i nije podešen; Cloudflare čuva svoje logove po svojim pravilima. | `internal/telegram/bot.go:184,201,281`, `cmd/server/main.go:70-97` | infra | P2 | Na VPS-u ograničiti journald (npr. `MaxRetentionSec=30day`), navesti rok u politici. | S |

## Neprovereno
- Sadržaj Poverenikove liste vrsta radnji obrade za koje je procena uticaja obavezna (čl. 54 st. 5 ZZPL) – da li izričito obuhvata lokaciju zaposlenih. Nisam našao tekst; pravnik da proveri na poverenik.rs.
- Da li je SAD i dalje na Vladinoj listi država sa primerenim nivoom zaštite („Sl. glasnik RS“ 55/2019). Izvori iz 2020. kažu da je Poverenik tražio uklanjanje SAD posle presude Schrems II, nije potvrđeno da je lista izmenjena. Isto za UAE. Do potvrde tretirati SAD i UAE kao zemlje bez primerenog nivoa.
- Koje pravno lice je ugovorna strana i obrađivač za Telegram Bot API (prema Telegramovim uslovima, verovatno Telegram FZ-LLC, Dubai) i da li Telegram uopšte nudi DPA za botove. Verovatno ne nudi.
- Ko upravlja Web3Forms, gde se čuvaju poruke i koliko dugo.
- Da li Cloudflare-ov DPA i klauzule važe kao „odgovarajuće mere“ po čl. 65 ZZPL (one su po GDPR SCC, ne Poverenikove).
- Da li demo preko tunela sa laptopa trenutno sadrži stvarne podatke klijenata (ako da, laptop je produkciono okruženje i treba enkripcija diska i backup).
- Da li Cloudflare na sajt automatski ubacuje Web Analytics/beacon (podešavanje u Cloudflare panelu, nije vidljivo u repou).
- Tačni članovi Zakona o elektronskoj trgovini, Zakona o PDV-u i Zakona o elektronskom fakturisanju koji se pominju ispod.
- Da li je evidencija radnji obrade (čl. 47) za obrađivača sa manje od 250 zaposlenih obavezna u ovom slučaju (uslovi iz čl. 47 st. 7 nisu analizirani do kraja).
- Da li GDPR važi (LEG-09). Ovo je pravno pitanje, ne tehničko.

## Dodatne napomene

### 1. Mapa podataka (iz koda, stanje 9c28a39)
| Podatak | Lica | Gde (tabela/mesto) | Ko pristupa | Rok čuvanja danas | Treće strane |
|---|---|---|---|---|---|
| Ime vozača, oznaka vozila, aktivan | vozači | `drivers` (0002) | owner, dispečeri firme; Dani90 (DB) | zauvek (samo deaktivacija) | – |
| Telegram chat ID vozača | vozači | `drivers.telegram_chat_id` | sistem; nije prikazan u UI | zauvek, nema odvezivanja | Telegram (izvor) |
| Datumi ulaska/izlaska, tip (posao/privatno), izvor, napomena | vozači | `stays` (0003/0011) | owner, dispečeri; vozač vidi svoje preko bota | zauvek; brišu se pojedinačno ručno | Telegram (poruke sa datumima, statusom) |
| Događaji i lokacija (lat/lng, zemlja, `raw` JSON) | vozači | `events` (0004) | – | tabela postoji, ne puni se | Telegram (budući izvor) |
| Email, ime, bcrypt hash lozinke, uloga, Telegram chat ID | owner/dispečer | `users` (0001, 0009) | owner firme; Dani90 | zauvek, nema brisanja | Telegram (ako poveže) |
| Sesije (token, CSRF secret, istek 14 dana) | korisnici | `sessions` (0008), kolačić `shengen_session` | sistem | istekle se ne brišu | – |
| Link tokeni (48 h, jednokratni) | vozači/korisnici | `telegram_link_tokens` (0005) | sistem | zauvek | link se šalje Viberom/SMS-om (van sistema) |
| Audit (ko, šta, kada; bez vrednosti) | korisnici, vozači | `audit_log` (0006) | owner preko `/ispravi` u botu | zauvek | – |
| Log poslatih upozorenja | vozači | `telegram_alert_log` (0010) | sistem | zauvek | – |
| Podešavanja firme (chat za upozorenja, pragovi) | firma | `tenants.settings` | owner | zauvek | – |
| IP + email pokušaja prijave | korisnici | memorija (`auth/ratelimit.go`, 15 min) | sistem | 15 min, do restarta | – |
| Logovi (chat ID, ID-jevi, greške) | vozači, korisnici | stdout → terminal/journald | Dani90 | nedefinisano | Cloudflare (svoji logovi) |
| CSV istorije (ime vozača, datumi) | vozači | samo u memoriji tokom uvoza | korisnik koji uvozi | ne čuva se fajl | Telegram (ako se šalje botu) |
| Kontakt forma: ime, firma, telefon, email, broj vozača, poruka | potencijalni kupci | Web3Forms → email preduzetnika | Dani90 | nedefinisano | Web3Forms, email provajder |
| Kalkulator: datumi putovanja | posetioci | `localStorage` u pregledaču | samo posetilac | dok korisnik ne obriše | nema (`site/kalkulator.html:123`) |
| IP/UA posetilaca app-a | korisnici | – | – | – | Google Fonts, unpkg (LEG-07) |
| Backup baze (planiran) | svi | Litestream → R2 ili Hetzner Object Storage | Dani90 | nedefinisano | Cloudflare (SAD) ili Hetzner (EU) |

Posebne vrste podataka (zdravlje, JMBG, broj pasoša) se ne prikupljaju. Rizik je slobodno polje `note`.

### 2. Uloge
- **Podaci vozača i boravaka:** firma (poslodavac) = rukovalac, Dani90 = obrađivač. Potvrđeno: firma određuje svrhu (kontrola 90/180 za svoje vozače) i ko se unosi; Dani90 obrađuje po njenom nalogu. Uslov: Dani90 ne sme te podatke koristiti za svoje svrhe (statistika, marketing, trening) bez anonimizacije i osnova; to piše u DPA.
- **Nalozi korisnika (owner/dispečer), naplata, podrška, logovi bezbednosti:** Dani90 = rukovalac (osnov: ugovor sa firmom i legitimni interes za bezbednost).
- **Kontakt forma, marketing sa sajta:** Dani90 = rukovalac (osnov: preduzimanje radnji na zahtev lica pre zaključenja ugovora / legitimni interes).
- **Javni kalkulator:** podaci ostaju u pregledaču, Dani90 ih ne prima, pa nema obrade od strane Dani90 (osim tehničkih logova hostinga, Cloudflare).
- **Telegram:** sam je rukovalac za svoje podatke korisnika; za poruke bota praktično deluje kao podobrađivač bez DPA. Pravnik da odluči kako to opisati (predlog: navesti u DPA kao kanal koji firma bira i za koji prihvata Telegramove uslove).
- **Vozač koji sam pritiska dugmad** ne postaje rukovalac; i dalje je lice na koje se podaci odnose.

### 3. Pravni osnov
- **Evidencija boravaka (bez lokacije):** za firmu verovatno legitimni interes (čl. 12 st. 1 tač. 6 ZZPL) da izbegne prekoračenje boravka zaposlenih na službenom putu i posledice po vozača i vozilo; moguće i izvršenje ugovora o radu. „Zakonska obaveza“ (tač. 3) je slabija: obaveza poštovanja 90/180 je na vozaču kao putniku, ne izričita obaveza poslodavca da vodi evidenciju; pravnik da proveri da li neki propis o radu u drumskom saobraćaju to menja. Pristanak (čl. 15) ne preporučujem kao osnov za zaposlene: odnos nije ravnopravan, a pristanak se može povući u svakom trenutku, što ruši evidenciju.
- **Privatna putovanja:** ulaze u isti limit pa su nužna za tačan obračun, ali poslodavac saznaje nešto o privatnom životu. Dovoljni su datumi; tip „privatno“ i napomena nisu nužni (LEG-16).
- **Lokacija:** legitimni interes uz obaveznu procenu interesa (LIA) i DPIA, lokacija samo na zahtev vozača (dugme „Pošalji lokaciju“) ili uključivanje koje vozač kontroliše, samo kod zemlje, bez koordinata, bez praćenja van radnog vremena. Čl. 91 ZZPL upućuje na propise o radu i posebne mere zaštite u radnoj sredini; pravnik da proveri Zakon o radu i eventualni kolektivni ugovor.
- **DPIA (čl. 54 ZZPL / čl. 35 GDPR):** za sadašnju evidenciju datuma verovatno nije obavezna (nema profilisanja sa pravnim posledicama, nema posebnih podataka, nema nadzora javnih površina), ali je preporučljiva kratka procena. Za lokaciju zaposlenih: tretirati kao obaveznu (sistematsko praćenje zaposlenih, nova tehnologija). DPIA radi rukovalac (firma); Dani90 treba da da šablon i podatke (čl. 45 st. 4 tač. 6).
- **Lice za zaštitu podataka (čl. 56 ZZPL / čl. 37 GDPR):** obavezno ako se osnovne aktivnosti sastoje od redovnog i sistematskog nadzora velikog broja lica. Sa nekoliko desetina vozača verovatno nije obavezno; sa lokacijom i stotinama vozača, granica postaje upitna. Pravnik da proceni po rastu; do tada odrediti kontakt za privatnost (email).
- **GDPR i predstavnik:** vidi LEG-09. ZZPL ima sličnu odredbu za strane rukovaoce (čl. 44), ali ona ovde ne važi jer je Dani90 u Srbiji.

### 4. Prenos podataka van Srbije
| Primalac | Šta | Država (pretpostavka) | Osnov | Šta treba |
|---|---|---|---|---|
| Hetzner (VPS, Object Storage) | sve | Nemačka/Finska (EU) | čl. 64 st. 2 ZZPL (članice Konvencije 108) | Hetzner DPA (nudi ga u konzoli), izabrati EU lokaciju, ne SAD/Singapur |
| Cloudflare (DNS, tunel/proxy, Pages, eventualno R2) | sav saobraćaj app-a, sajt, backup ako R2 | SAD + globalno | čl. 65 – treba odgovarajuća mera | Prihvatiti Cloudflare DPA; pravnik: da li je dovoljan za ZZPL; razmotriti „DNS only“ za app posle prelaska na VPS (Caddy radi TLS) da Cloudflare ne vidi sadržaj |
| Telegram | ime vozača u porukama, datumi, status, chat ID | UAE/globalno (neprovereno) | čl. 65 teško ostvariv (nema DPA) | Obavestiti lica, minimizovati sadržaj poruka (npr. upozorenje dispečeru sa imenom je nužno), firma u DPA prihvata Telegram kao kanal; alternativa za vozače bez Telegrama |
| Web3Forms | upit sa sajta | neprovereno | neprovereno | Proveriti operatora; alternativa: sopstveni endpoint na VPS-u + email |
| Google Fonts, unpkg | IP, UA korisnika app-a | SAD | nema | Ukloniti (LEG-07) |
| Email provajder preduzetnika | upiti, fakture | zavisi | – | Navesti u politici |

### 5. Prava lica – stanje u kodu
| Pravo | ZZPL | Stanje | Dokaz |
|---|---|---|---|
| Pristup | čl. 26 | delimično: vozač u botu vidi broj dana; firma vidi sve; nema izvoza | `internal/telegram/driver.go:26` |
| Ispravka | čl. 29 | delimično: izmena boravka da; izmena imena/vozila vozača ne postoji | `internal/web/server.go:53` |
| Brisanje | čl. 30 | ne za vozača/korisnika/firmu; samo pojedinačni boravak | `internal/store/stay.go:216`, `internal/store/driver.go:103` |
| Ograničenje | čl. 31 | deaktivacija vozača je približno to | `internal/web/server.go:58` |
| Prenosivost | čl. 36 | ne (samo uvoz CSV, nema izvoza) | `internal/web/server.go:70-72` |
| Prigovor | čl. 37 | nema procesa | – |

### 6. Disclaimer danas
Kratka rečenica postoji u footeru app-a (`layout.html:40`), na pomoći (`pomoc.html:115`), sajtu (`index.html:259,290,378`) i kalkulatoru (`kalkulator.html:165,197,224`). Nema je u botu i nema ugovornog ograničenja odgovornosti. Predlog teksta: `legal-drafts/disclaimer-kalkulacija.md`.

### 7. Predloženi rokovi čuvanja (za pravnika da potvrdi)
- Boravci aktivnog vozača: dok traje ugovor; boravci stariji od 180 dana nisu potrebni za obračun, ali firma ih može hteti za dokazivanje (predlog: 2 godine, pa brisanje).
- Deaktiviran vozač: 12 meseci, pa trajno brisanje (firma može ranije).
- Firma posle prestanka ugovora/triala: 30 dana za izvoz, pa brisanje (UX predlaže 12 meseci za neaktivne triale; za plaćene posle raskida kraće je bezbednije).
- Istekle sesije i link tokeni: brisati dnevno. Alert log: 90 dana. Audit log: 2 godine. Logovi servera: 30 dana. Backup: rotacija 30 dana.
- Upiti sa sajta: 12 meseci ako ne postanu kupci.
- Računovodstvena dokumentacija (fakture): po propisima o računovodstvu (knjigovođa).

### 8. Minimum za fakturu (proveriti sa knjigovođom)
- Faktura od preduzetnika ka firmi u Srbiji: naziv i adresa radnje, PIB i MB izdavaoca i kupca, broj i datum fakture, datum prometa, opis usluge („Pretplata Dani90 – N vozača, period od–do“), količina, cena, iznos, tekući račun, poziv na broj.
- Ako preduzetnik nije u sistemu PDV-a (paušalac ili promet ispod praga od 8 miliona dinara u 12 meseci): bez PDV-a, uz napomenu da obveznik nije u sistemu PDV-a (tačan tekst i član: knjigovođa). Ako jeste u PDV-u: obračun 20%.
- SEF: od 2023. B2B fakture između subjekata privatnog sektora izdaju se kao e-fakture preko SEF-a, i to važi i za preduzetnike koji nisu u PDV-u, uključujući paušalce, koliko znam. Knjigovođa da potvrdi i registruje radnju na SEF.
- Cena je u evrima (€3,99): u Srbiji se faktura domaćem kupcu po pravilu iskazuje u dinarima (moguće uz valutnu klauzulu). Knjigovođa da potvrdi.
- Fiskalni račun (fiskalizacija) se uglavnom ne izdaje za bezgotovinsko plaćanje pravnog lica preko računa; knjigovođa da potvrdi.
- Paušalac: proveriti da li delatnost (razvoj softvera/SaaS) i nezavisnost od kupaca (test samostalnosti) dozvoljavaju paušalno oporezivanje.

### 9. Šta mora pravnik / knjigovođa
**Pravnik:** pregled svih pet nacrta; primena GDPR po čl. 3(2)(b) i potreba za predstavnikom u EU; osnov za evidenciju i lokaciju zaposlenih (Zakon o radu, čl. 91 ZZPL); DPIA šablon za firme pre lokacije; osnov za prenos u SAD/UAE (Cloudflare, Telegram) i da li Vladina lista i dalje sadrži SAD; obavezni podaci po Zakonu o elektronskoj trgovini; domašaj ograničenja odgovornosti prema ZOO i Zakonu o zaštiti potrošača (ako kupac može biti preduzetnik-fizičko lice); da li je evidencija radnji obrade obavezna; rokovi čuvanja.
**Knjigovođa:** PDV status i napomena na fakturi; SEF registracija i obaveza; dinari ili evri; fiskalizacija; uslovi za paušal; čuvanje fakture.

### Izvori
- ZZPL, „Sl. glasnik RS“ 87/2018, tekst: https://region.vojvodina.gov.rs/wp-content/uploads/2023/07/zakon_o_zastiti_podataka_o_licnosti_13741.docx (čl. 3, 12, 15, 23, 26, 29–31, 36–37, 44–45, 47, 52–56, 64–65, 91 provereni u tekstu)
- Poverenik o SAD posle Schrems II: https://nova.rs/vesti/drustvo/poverenik-sad-ne-obezbeduju-primeren-nivo-zastite-podataka/ , https://zuniclaw.com/privacy-shield-eu-us/
- GDPR čl. 3 i 27: https://eur-lex.europa.eu/eli/reg/2016/679/oj

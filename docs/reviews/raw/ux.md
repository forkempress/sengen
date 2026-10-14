<!-- Tekst izvučen iz docs/reviews/raw/ux.html (original UX review, artifact https://claude.ai/artifact/1ts6dMaMxWVaJz1qH3V5LX). Slike: docs/ux-screens/. -->

Dani90 UX pregled

# Dani90 UX pregled

Pregled web aplikacije (app.dani90.rs), Telegram bota i prodajnog sajta, plus predlog logina, samostalne registracije i mini design system-a. Stanje koda: commit 9c28a39, 14.10.2026. App je pokrenut lokalno sa demo podacima (firma sa 10 vozača i prazna firma); screenshotovi za 1440px i 390px su napravljeni headless Chrome-om.

 SažetakInventarReview po straniciLoginRegistracijaDashboardDesign systemLista izmena

## Executive summary

Tri najveća problema: (1) na telefonu dispečer na Pregledu ne vidi preostale dane: kolona je van ekrana, vidi se samo ime i „gde je“; (2) status se prenosi samo bojom i brojem u malom kružiću, narandžasti badge ima kontrast 2,8:1, a nigde ne piše „OK / Upozorenje / Prekoračeno“; (3) „Obriši boravak“ i „Deaktiviraj“ rade bez potvrde, jer hx-confirm ne radi na običnim formama.

Tri najbrža dobitka: tekstualna oznaka statusa uz broj i tamnije boje statusa (S); kartice umesto tabele na mobilnom Pregledu (S–M); prava potvrda brisanja, brendirane 404/500 stranice i polja za autocomplete na loginu (S).

Jezgro je dobro: sortiranje po riziku, HITNO baner, kalendar i provera ture su korisniji nego kod većine konkurenata. Problemi su uglavnom u prezentaciji i u tome što app nema nijedan ekran za nekoga ko još nema nalog.

## 1. Inventar stranica i poruka

 | Ruta / mesto | Ko vidi | Šta je | Screenshot

 | /login | svi | Forma email + lozinka, greška „Pogrešan email ili lozinka“, rate limit 5/15 min | da

 | / → /pregled | vlasnik, dispečer | Semafor vozača: KPI kartice, filteri, tabela, HITNO baner, „stanje na dan“ | da (pun, prazan, filter bez rezultata, budući dan)

 | /vozaci/{id} | vlasnik, dispečer | Kartice, „Vratio se danas“, provera ture, kalendar 6 meseci, dodaj boravak, istorija sa izmenom/brisanjem | da (u Šengenu, crven, bez boravaka, rezultat ture)

 | /podesavanja/vozaci | vlasnik, dispečer | Dodaj vozača, lista, Telegram link, deaktivacija | da (pun, prazan)

 | /podesavanja/korisnici | vlasnik | Dodaj korisnika (početna lozinka), lista | da

 | /podesavanja/telegram | vlasnik | Kome idu upozorenja, pragovi, chat ID, poveži svoj Telegram | da

 | /import | vlasnik, dispečer | CSV uvoz: pregled sa greškama po redu, potvrda | da (bez fajla); pregled sa greškama neprovereno

 | /pomoc | vlasnik, dispečer | Uputstvo, legenda boja, objašnjenje kolona | da

 | 404 / 403 / 500 | svi | Go podrazumevani tekst 404 page not found; 500 je goli tekst „greška servera“ | da (404); 500 iz koda

 | Flash poruke | svi | Zelena / crvena traka na vrhu sadržaja | iz koda

 | Telegram: vozač | vozač | Dugmad Izašao iz Srbije / Vratio se u Srbiju / Moji dani / Pomoć, potvrde, „Poništi“ 10 min, podsetnik posle 7 dana | iz koda (telegram/driver.go, help.go)

 | Telegram: dispečer | vlasnik, dispečer | /pregled /vozac /planer /dodaj /link /ispravi /sablon, brzi unos teksta, CSV, upozorenja i dnevni pregled u 07:00 | iz koda (admin.go, jobs.go)

 | Email poruke | — | Ne postoje: app ne šalje email (nema reseta lozinke ni verifikacije) | —

 | Sajt dani90.rs | javno | index, kalkulator, hvala, 404; CTA je „Zakažite demo“ (web3forms), nema registracije | da

Svih 40 screenshotova (20 stranica × desktop/mobil) je u docs/ux-screens/ u repou. Ovde su prikazani samo oni koji pokazuju problem.

## 2. Review po stranici

[img img/pregled-desktop.jpg alt=Pregled na desktopu: KPI kartice, filteri i tabela vozača; kolona Izvor je odsečena desno]Pregled, 1440px. Tabela je šira od kontejnera od 1100px, pa je kolona „Izvor“ odsečena. Link u HITNO baneru je plav na tamnocrvenom.

[img img/pregled-mobil.jpg alt=Pregled na telefonu: vide se samo kolone vozač i gde je]Pregled, 390px. Preostali dani se ne vide bez horizontalnog skrolovanja.

Pregled /pregled

 | Dobro | Problem | Sugestija | Prioritet

 | Sortiranje po riziku. HITNO baner na vrhu. „Može do“ i „najduža tura“ odgovaraju na pravo pitanje dispečera. Filteri i „stanje na dan“ su korisni, a pretraga radi bez kvačica. | Na 390px vidljive su samo 2 od 7 kolona; „Preostalo“ je van ekrana. | Ispod 700px prikazati listu kartica: ime + veliki broj preostalih dana + tekstualni status, u drugom redu „U Šengenu od 07.10 · može do 24.10“. Wireframe u sekciji Dashboard. | P1

 | Status je samo broj u obojenom kružiću; reč postoji samo za 0 („HITNO“). Daltonista ne razlikuje 18 od 38. | Badge = ikona + reč + broj: „✓ OK 38“, „! Pažnja 18“, „▲ Kritično 6“, „⛔ Preko limita −6“. Ista reč u botu i upozorenjima. | P1

 | Narandžasti badge: beo tekst na #D88B12 = 2,8:1; zeleni 4,3:1. Oba padaju WCAG AA za mali tekst. | Svetla pozadina + taman tekst (paleta u design system-u), svi parovi ≥ 4,5:1. | P1

 | Link vozača u HITNO baneru: plavo na tamnocrvenom = 1,1:1, praktično nevidljivo. | Beo ili svetložut podvučen link, plus dugme „Otvori vozača“. | P1

 | Na desktopu tabela izlazi iz kontejnera; „Izvor“ je odsečen i ima malu vrednost za dispečera. | Ukloniti „Izvor“ iz pregleda (ostaje na stranici vozača) ili proširiti kontejner na 1280px. | P2

 | KPI kartice su sive i ne vode nigde. „Ispod 15 dana: 3“ ne kaže koji su to vozači. | Kartice kao filteri (klik → ?ozbiljnost=crveno), leva traka u boji statusa, broj HITNO kao posebna kartica. | P2

 | Terminologija: filter kaže „žuto“, badge je narandžast, Telegram stranica kaže „narandžasto“. Kolona „Iskorišćeno/90“ i „Preostalo“ su dva lica istog broja. | Imenovati statuse, ne boje: OK / Pažnja / Kritično / Preko limita. Jedan pojam za datum: „Mora da izađe do“ (u Šengenu) i „Najduža tura“ (u Srbiji). | P2

 | Prazno stanje: četiri nule, filteri i tabela sa jednim redom „Nema vozača“. | Sakriti KPI i filtere dok nema vozača; prikazati kontrolnu listu prvih koraka (vidi prazno stanje). | P1

[img img/vozac-u-sengenu-desktop.jpg alt=Stranica vozača na desktopu: četiri kartice, dugmad, provera ture, kalendar, istorija]Vozač, 1440px. Najvažniji broj (41 preostalo) je najmanji element u redu kartica.

[img img/vozac-crven-mobil.jpg alt=Stranica vozača na telefonu, gornji deo]Vozač, 390px. Stranica je šira od ekrana (404px), kalendar je 6 meseci jedan ispod drugog.

Vozač /vozaci/{id}

 | Dobro | Problem | Sugestija | Prioritet

 | „Vratio se danas“ kao primarno dugme. Kalendar sa projekcijom i danima oslobađanja je jedinstvena vrednost. Provera ture daje jasan odgovor „Može / Ne može“ i najraniji polazak. | „Obriši“ i „Deaktiviraj“ nemaju potvrdu: hx-confirm radi samo na htmx zahtevima, a forme su obične method="post". Jedan pogrešan klik briše boravak bez povratka. | Dodati hx-post na te forme ili onsubmit="return confirm(…)"; bolje: brisanje sa „Poništi“ u flash poruci 10 s, kao u botu. | P1

 | „Preostalo danas“ je mali badge (13px) u kartici, dok su datumi pored 26px. | Hero blok na vrhu: veliki broj preostalih dana, status rečju, jedna rečenica („U Šengenu od 08.10. Mora da izađe najkasnije 06.12.2026.“). | P1

 | Na 390px stranica je 14px šira od ekrana; istorija skriva dugmad Izmeni/Obriši desno. | Istorija kao lista kartica na mobilnom; kalendar 2–3 meseca uz „Prikaži još“. | P2

 | Labele „Ulazak“, „Izlazak“, „Tip“, „Napomena“ nemaju for/id, pa ih čitač ekrana ne povezuje sa poljem. | Dodati id i for svuda (ista greška je u Vozači i Uvoz). | P2

 | Crveno „Obriši“ dugme u svakom redu je dominantnije od samih podataka. | Sekundarno dugme ili meni „⋯“; crveno tek u koraku potvrde. | P3

 | Redosled: provera ture je iznad kalendara i unosa, a dispečer najčešće unosi ili ispravlja boravak. | Hero → akcije (Vratio se / Dodaj boravak) → kalendar → istorija → provera ture u sklopivom bloku ili u modalu. | P3

 | Vozač bez boravaka vidi prazan kalendar i „Još nema unetih boravaka“. | Poruka: „Unesite boravke iz poslednjih 180 dana da bi brojanje bilo tačno“ + dugmad „Dodaj boravak“ i „Uvezi CSV“. | P2

[img img/telegram-desktop.jpg alt=Podešavanja Telegram stranica]Telegram i pragovi. „Chat ID“ je tehnički pojam za korisnika koji nije tech-savvy.

[img img/prazno-pregled-desktop.jpg alt=Prazan pregled sa nulama]Prvi ekran nove firme: nule i jedna rečenica.

Vozači, Korisnici, Telegram, Uvoz, Pomoć, greške

 | Stranica | Dobro | Problem | Sugestija | Prioritet

 | Vozači | Kratka forma, jasna akcija „Telegram link“. | Na mobilnom kolone Status i dugmad su van ekrana. Nema kolone „Telegram povezan da/ne“, a to je najvažnija informacija ove stranice. Naslov „Podešavanja — Vozači“ a stavka menija „Vozači“. | Kolona „Telegram: ✓ povezan / ✗ nije“ sa dugmetom pored. Posle „Dodaj vozača“ odmah prikazati link i QR. Naslov samo „Vozači“. | P1

 | Korisnici | Uloge su objašnjene jednom rečenicom. | Vlasnik smišlja i prenosi lozinku drugoj osobi. Nema brisanja ni promene uloge. | Poziv emailom („Pošalji pozivnicu“), korisnik sam bira lozinku. Zavisi od slanja emaila (isto što treba registraciji). | P2

 | Telegram | Status „Upozorenja se ne šalju nikome“ je jasan i istaknut. | Stranica meša tri teme: kome idu upozorenja, pragove i povezivanje sopstvenog naloga. „Chat ID“ i „-1001234567890“ ništa ne znače vlasniku firme. Redosled tera korisnika da ide dole pa gore. | Preimenovati u „Upozorenja“. Korak 1: „Poveži svoj Telegram“ (QR + dugme). Korak 2: „Šalji upozorenja meni / u grupu“ (grupa preko t.me/bot?startgroup=, bez ručnog ID-a). Pragovi posebno, kao klizač sa primerom. | P2

 | Uvoz | Pregled pre upisa i „ništa se ne upisuje dok ne potvrdite“. | Nema šablona za preuzimanje. Kolone su opisane u jednom dugom pasusu. Polje za fajl je goli native input. | Dugme „Preuzmi primer CSV“, tabela sa primerom, drag-and-drop zona. Prikaz grešaka po redu neprovereno vizuelno. | P2

 | Pomoć | Najbolja stranica u app-u: tačna, konkretna, sa primerima i legendom. | Odvojena od mesta gde je potrebna. | Ikona „?“ pored naslova kolona na Pregledu vodi na odgovarajuće sidro u Pomoći. | P3

 | 404 / 500 | — | Goli tekst na engleskom, bez navigacije (vidi screenshot ispod). Na mobilnom stranica je 980px široka. | Šablon greške u layoutu: „Ova stranica ne postoji“ / „Nešto nije u redu, pokušajte ponovo“ + dugme „Na pregled“. | P2

 | Navigacija | Aktivna stavka je istaknuta. | Na 390px zaglavlje zauzima 150px u tri reda. Fokus stanja se oslanjaju na podrazumevane browser stilove; ulazna polja imaju ivicu 1,3:1 (WCAG traži 3:1 za granice kontrola). | Na mobilnom: logo + „Meni“ dugme; donja traka sa Pregled / Vozači / Pomoć. Globalno :focus-visible (žuti obrub kao na sajtu), ivica polja #8C958C. | P2

 | Stanja | Flash uspeha i greške postoje. | Nema loading stanja (dugme ne pokazuje da se šalje, pa na slabom signalu korisnik klikne dvaput). Flash nestaje tek na sledećoj stranici. | Dugme dobija disabled + „Čuvam…“ pri submitu; server već odbija duplikate, ali korisnik to ne vidi. | P3

[img img/vozaci-mobil.jpg alt=Lista vozača na telefonu bez kolone akcija]Vozači, 390px: dugmad za Telegram i deaktivaciju nisu vidljiva.

[img img/404-app-mobil.jpg alt=Podrazumevana Go 404 stranica]404 u app-u.

Telegram bot (iz koda)

 | Dobro | Problem | Sugestija | Prioritet

 | Četiri velika dugmeta, „Poništi“ 10 minuta, uputstvo objašnjava BiH/Makedoniju. Potvrda posle unosa kaže do kad vozač sme da ostane. | „Izašao iz Srbije“ znači „ušao u Šengen“. Uputstvo to ispravlja, a dugme vozača navodi na pogrešan klik na Batrovcima pri ruti preko BiH. | „▶ Ušao u Šengen“ / „◀ Izašao iz Šengena“. Isti termin u webu: „ulazak / izlazak“. | P1

 | Mešanje „ti“ i „Vi“ u istom razgovoru („Već imaš…“ / „pokušajte ponovo“). | Vozaču svuda „ti“, dispečeru svuda „Vi“. | P3

 | „Da li si još u EU?“, a pravilo je Šengen. Kipar i Irska su EU a ne broje se, što app sam naglašava. | „Da li si još u Šengenu?“ | P2

 | Vozač ne dobija upozorenje kad mu ostane malo dana, samo dispečer. | Poruka vozaču na pragu: „Ostalo ti je 9 dana. Moraš da izađeš najkasnije 24.10.“ | P2

Prodajni sajt dani90.rs

Sajt je vizuelno najzreliji deo (brend, logo, fontovi, fokus stanja, FAQ). Za samostalnu registraciju treba: CTA „Probajte besplatno 30 dana“ pored „Zakažite demo“, cena na sajtu (sada samo pitanje u FAQ), i da login stranica app-a preuzme isti logo i žutu. Placeholder-i u footeru („kontakt — upisati pre objave“, „naziv firme i PIB“) su vidljivi na stranici P1 pre objave.

## 3. Login stranica

[img img/login-desktop.jpg alt=Trenutni login: siva pozadina i kartica sa dva polja]Sada: nema logoa ni imena proizvoda, nema „zaboravljena lozinka“ ni registracije. Polja nemaju autocomplete="email" / "current-password", pa password manageri na telefonu rade lošije.

Šta dodati, po redu važnosti:

- Logo „90“ + Dani90 i jedna rečenica: „Koliko dana vaši vozači još smeju u Šengenu.“

- „Zaboravili ste lozinku?“ (traži slanje emaila; danas resetovanje radi samo vlasnik preko seed komande).

- „Nemate nalog? Registrujte firmu besplatno.“

- Prijava preko Telegrama (Telegram Login Widget): dispečeri već imaju povezan Telegram, pa jedan tap zamenjuje lozinku. Widget traži domen podešen kod BotFather-a (/setdomain).

- Trust red: „Podaci se čuvaju u EU · Ne delimo podatke · Kontakt: …“. „Koristi nas X firmi“ tek kad X bude ≥ 20.

- Greška pored polja, ne samo na vrhu; dugme „Prikaži lozinku“.

### Varijanta A: minimalna

app.dani90.rs/login · A

90Dani90

Koliko dana vaši vozači još smeju u Šengenu.
 Email
ime@firma.rs

LozinkaZaboravili ste lozinku?
••••••••••

Prijavi se

Nemate nalog? Registrujte firmu besplatno

### Varijanta B: srednja (preporuka)

app.dani90.rs/login · B · na telefonu se leva strana sakriva

90Dani90

Znajte tačno koliko dana vaš vozač još sme u Šengenu.

Marko J. · BG 1234-AB▲ Kritično 6

Nikola P. · BG 2271-KM! Pažnja 22

Dejan K. · ČA 774-RS✓ OK 41

Vozač javlja granicu jednim dugmetom u Telegramu. Podaci se čuvaju u EU.

Prijava
 Prijavi se preko Telegrama

ili emailom
 Email
ime@firma.rs

LozinkaZaboravili ste lozinku?
••••••••••

Prijavi se

Nemate nalog? Registrujte firmu besplatno · 30 dana bez kartice

### Varijanta C: bogata

Kao B, plus: na levoj strani kratka animacija dugmeta „Ušao u Šengen“ u telefonu, citat klijenta, logotipi firmi, i ispod forme link „Pogledajte demo bez prijave“ (read-only demo firma). Ima smisla tek sa 20+ klijenata i kad demo firma bude stabilna; do tada je B bolji odnos uloženog i dobijenog.

Preporuka: B. Mini-pregled sa tri vozača je istovremeno brend i objašnjenje proizvoda, a Telegram dugme skida najveći problem netehničkih korisnika: zaboravljene lozinke. Ako slanje emaila još ne postoji, pustiti prvo A bez „Zaboravili ste lozinku?“ (link „Javite nam se“ do tada).

## 4. Samostalna registracija

### Tok

 | Korak | Ekran | Polja / akcija | Cilj vremena

 | 0 | Sajt: CTA „Probajte 30 dana besplatno“ | vodi na app.dani90.rs/registracija | 0:00

 | 1 | Registracija | Ime firme, email, lozinka (min. 10), čekboks uslova; Turnstile nevidljiv | 0:30

 | 2 | „Proverite email“ | Kod od 6 cifara u istom tabu (bolje od linka na telefonu sa drugim browserom) + link kao rezerva | 1:00

 | 3 | Wizard 1/4 — Firma | Vaše ime, broj vozača (okvirno, za cenu); sve ostalo kasnije | 1:15

 | 4 | Wizard 2/4 — Prvi vozač | Ime, registracija (opciono). Dugme „Dodaću više kasnije“ | 1:35

 | 5 | Wizard 3/4 — Telegram | QR + „Pošalji link vozaču“ (Viber/SMS) + „Preskoči za sada“. Ekran sam prelazi dalje kad vozač tapne | 2:10

 | 6 | Wizard 4/4 — Poslednji ulazak | „Kad je vozač poslednji put ušao u Šengen?“ + „Još je tamo / Vratio se dana…“; „Imam CSV“ kao alternativa | 2:40

 | 7 | Rezultat (aha momenat) | Veliki broj preostalih dana, status rečju, „mora da izađe do“; dugme „Na pregled“ | < 3:00

Telegram korak se može preskočiti, a unos ulaska ne: bez jednog boravka nema broja, a broj je ceo „aha“. Ako vozač nije pri ruci, dispečer unosi datum sam.

### Wireframe-ovi

/registracija

90Dani90

Registrujte firmu

30 dana besplatno, bez kartice. Posle 30 dana odlučujete.
 Ime firme
npr. Prevoz Jovanović
 Email
ime@firma.rs
 Lozinka
najmanje 10 znakova

☐ Prihvatam uslove i politiku privatnosti

Napravi nalog

Već imate nalog? Prijava

/registracija/potvrda

Proverite email

Poslali smo kod na ime@firma.rs. Upišite ga ovde.

4
8
1

Potvrdi

Nije stiglo? Pogledajte „Promocije“ ili spam. Pošalji ponovo (za 60 s) · Pogrešan email?

/pocetak/vozac

✓ Firma2 Vozač3 Telegram4 Ulazak

Dodajte prvog vozača
 Ime i prezime
Marko Jovanović
 Registracija vozila (nije obavezno)
BG 1234-AB

Imam spisak u ExceluDalje

/pocetak/telegram

✓ Firma✓ Vozač3 Telegram4 Ulazak

Povežite Marka sa botom

Ako je Marko pored vas: neka skenira kod telefonom.

Ako nije: pošaljite mu link, on samo tapne.

t.me/dani90bot?start=k7Qx…

Kopiraj linkViberSMS

⏳ Čekamo da Marko tapne… Ekran ide dalje sam.

Preskoči, povezaću kasnije

/pocetak/ulazak

✓ Firma✓ Vozač✓ Telegram4 Ulazak

Kad je Marko poslednji put ušao u Šengen?
 Datum ulaska
05.10.2026
 Gde je sada?

Još je u ŠengenuVratio se…

Za tačan broj unesite sve ulaske iz poslednjih 180 dana. Za početak je dovoljan jedan.

Izračunaj

/pocetak/rezultat

Marko Jovanović · BG 1234-AB

 6dana preostalo

 ▲ Kritično

U Šengenu je od 05.10.2026. Mora da izađe najkasnije 24.10.2026.

Bot će vam javiti 3 dana pre roka. Prvi dani mu se vraćaju 15.10.2026.

Na pregledDodaj još vozača

### Model besplatnog korišćenja

 | Model | Prednosti | Mane za ovu ciljnu grupu

 | Trial 14 dana | Brza odluka, kratak ciklus prodaje. | Vozač koji je sada u Srbiji ne pređe granicu za 14 dana; firma ne vidi ni jedno upozorenje. Unos istorije oduzme pola triala.

 | Trial 30 dana, svi vozači, bez kartice | Pokriva bar jednu turu i jedno upozorenje. Firma unese sve vozače, pa je trošak napuštanja veći. Prirodno se uklapa u mesečni ciklus fakturisanja. | Više neaktivnih naloga; zato treba podsetnik 3. i 10. dana ako nema unosa.

 | Besplatno do 2 vozača | Firma sa 1–2 kombija nikad ne mora da plati, pa preporučuje dalje. | Mnoge ciljne firme imaju 2–5 vozača; deo bi ostao zauvek besplatan. Firma deli flotu na dva naloga.

 | Freemium (pregled besplatno, upozorenja plaćeno) | Jasna vrednost plaćenog dela. | Upozorenje je ono što sprečava deportaciju. Skrivati ga iza naplate izgleda loše kad se nešto desi.

Preporuka: trial 30 dana bez kartice, bez ograničenja broja vozača. Uz €2/vozač mesečno, ograničenja po funkcijama ne vrede komplikacije u objašnjavanju.

### Kraj triala

 | Kada | Šta korisnik vidi

 | dan 1–20 | Diskretna traka u zaglavlju: „Probni period: još 24 dana“.

 | dan 23 | Email + Telegram vlasniku: „Probni period ističe 14.11. Za 8 vozača mesečno je €16. Pogledajte predračun.“ Traka postaje žuta.

 | dan 28, 30 | Isto, uz dugme „Pošalji mi predračun“.

 | dan 31–44 | Read-only za web: Pregled i stranice vozača rade, forme su zaključane uz poruku „Probni period je istekao. Pošaljite predračun“. Bot i dalje prima dugmad vozača (da istorija ne dobije rupu), ali upozorenja dispečeru staju.

 | dan 45+ | Nalog se pauzira; podaci se čuvaju 12 meseci, pa brišu uz najavu. Izvoz CSV ostaje dostupan.

Primer trake: Probni period ističe za 3 dana. 8 vozača × €2 = €16 mesečno. Pošalji predračun

Plaćanje: srpske firme plaćaju preko računa. Stranica /pretplata: broj aktivnih vozača, iznos, dugme „Pošalji predračun“ (PDF na email, sa pozivom na broj), status „Čeka uplatu / Plaćeno do 14.12.2026.“. Uplatu označavate ručno u admin delu. Da li fakture idu preko SEF-a (e-fakture) zavisi od vašeg PDV statusa i toga da li je kupac obveznik: neprovereno, proveriti sa knjigovođom.

### Zaštita od zloupotrebe

- Cloudflare Turnstile na registraciji i posle 3 neuspela logina. Nevidljiv režim; app je ionako iza Cloudflare tunela.

- Rate limit: registracija 3/sat po IP, slanje koda 5/sat po emailu. Postojeći limiter je u memoriji i resetuje se na restart; za registraciju čuvati u SQLite.

- Verifikacija emaila pre wizarda: dok nije potvrđena, nema slanja Telegram linkova (bot ne sme biti kanal za spam).

- Odbijanje disposable domena, limit od 10 vozača dok traje trial bez kontakta, i obaveštenje vama na Telegram za svaku novu registraciju.

- Preduslov: app sada ne šalje email. Treba transakcioni servis (Postmark, Resend, Amazon SES) sa SPF/DKIM za dani90.rs.

## Dashboard firme

/pregled · desktop i telefon (red se lomi u karticu ispod 640px)

90 Dani90PregledVozačiUpozorenjaPomoćPetar ▾

⛔ Miloš Đorđević je 6 dana preko limita i još je u Šengenu. Otvori vozača

Preko limita1

Kritično (< 15)2

Pažnja (15–30)2

OK (> 30)5

🔍 Ime ili registracijaStanje na dan: danas ▾

VozačPreostaloGde jeRok / tura

Miloš ĐorđevićBG 9930-ZX⛔ Preko −6U Šengenu od 07.10.Rok je bio 08.10.

Marko JovanovićBG 1234-AB▲ Kritično 6U Šengenu od 05.10.Mora da izađe do 24.10.

Nikola PetrovićBG 2271-KM! Pažnja 22U Šengenu od 10.10.Mora da izađe do 20.11.

Stefan NikolićNS 0412-TR✓ OK 48U SrbijiNajduža tura: 60 dana

### Prazno stanje posle registracije

/pregled · nova firma, wizard preskočen

Još 3 koraka do prvog pregleda

Firma je registrovana

Dodajte vozače — ručno ili iz Excel/CSV fajla

Unesite ulaske iz poslednjih 180 dana

Povežite vozače sa Telegram botom

Dodaj vozačaUvezi CSVPogledaj kako izgleda sa primerom

Isti princip za ostala prazna stanja: Vozači bez vozača (forma + „Uvezi spisak“), vozač bez boravaka (vidi tabelu iznad), Upozorenja bez povezanog Telegrama (jedno dugme „Poveži moj Telegram“, bez forme za pragove).

## 5. Mini design system

Zadržati postojeći smer (IBM Plex, teget, žuta, topla siva pozadina), jer ga već dele sajt i app. Menjaju se boje statusa i par komponenti.

### Paleta

Header / tekst
#16202B

Primarna (teget)
#1D3F91

Brend akcenat (žuta)
#F2B33D samo na tamnom

Pozadina
#EEF0EC

Ivica polja
#8C958C (3,0:1)

Sekundarni tekst
#55606A

### Statusi (boja + ikona + reč)

 | Status | Pravilo | Badge | Tekst / pozadina

 | OK | > 30 dana | ✓ OK 48 | #1F7A47 na #E3F3EA · 4,7:1

 | Pažnja | 15–30 | ! Pažnja 22 | #935600 na #FBEFD9 · 5,2:1

 | Kritično | 1–14 | ▲ Kritično 6 | #B3301A na #FBE9E6 · 5,3:1

 | Preko limita / 0 | ≤ 0 | ⛔ Preko −6 | beo na #7A1408 · 10,9:1

Pragovi ostaju podesivi; menja se samo naziv (status, ne boja) u filterima, botu i upozorenjima.

### Tipografija

H1 · IBM Plex Sans 600 · 24px

H2 · Plex Sans 600 · 18px

Tekst · Plex Sans 400 · 16px (sada 14–15px; na telefonu 16px sprečava zumiranje iOS-a u poljima)

Pomoćni · 13px, nikad ispod

41 · Plex Mono za brojeve i datume, tabular-nums

### Komponente

PrimarnoSekundarnoObrišiTelegramSva dugmad min. 44px visine; jedno primarno po ekranu.

Labela iznad polja (nije obavezno)
Placeholder samo kao primer, ne umesto labele
Greška ispod polja, rečenicom: „Izlazak ne može biti pre ulaska.“

Kartica sa statusom2
Neutralna kartica10Radius 6px, ivica 1px, bez senke. Statusna traka levo samo kad kartica nosi status.

## 6. Lista izmena po odnosu uticaj/napor

 | # | Izmena | Uticaj | Napor | Gde

 | 1 | Potvrda za „Obriši boravak“ i „Deaktiviraj“ (sada ih nema) | visok | S | vozac_detalj.html, podesavanja_vozaci.html

 | 2 | Status rečju + ikonom u badge-u; nove boje statusa (kontrast) | visok | S | status.go Badge(), style.css

 | 3 | Čitljiv link u HITNO baneru | visok | S | style.css .urgent-banner a

 | 4 | Dugmad bota „Ušao u Šengen / Izašao iz Šengena“; „još u Šengenu“ umesto „EU“ | visok | S | telegram/keyboards.go, jobs.go

 | 5 | Login: logo, rečenica, autocomplete, link za kontakt | srednji | S | login.html

 | 6 | Brendirane 404 / 500 stranice | srednji | S | render.go, novi greska.html

 | 7 | for/id na svim labelama, :focus-visible, tamnija ivica polja | srednji | S | šabloni, style.css

 | 8 | Pregled na mobilnom kao kartice | visok | M | pregled.html, CSS

 | 9 | Hero sa preostalim danima na stranici vozača; mobilna istorija | visok | M | vozac_detalj.html

 | 10 | Prazna stanja sa kontrolnom listom (Pregled, Vozači, vozač bez boravaka) | visok | M | šabloni

 | 11 | Kolona „Telegram povezan“ u Vozačima; QR posle dodavanja | srednji | M | podesavanja_vozaci.html

 | 12 | KPI kartice kao filteri; jedinstveni nazivi statusa u filteru | srednji | S | pregled.html

 | 13 | Mobilna navigacija (meni ili donja traka) | srednji | M | layout.html

 | 14 | Stranica „Upozorenja“ preuređena; grupa bez ručnog chat ID-a | srednji | M | podesavanja_telegram.html, bot

 | 15 | Šablon CSV za preuzimanje, drag-and-drop | nizak | S | import.html

 | 16 | Slanje emaila (preduslov za reset lozinke, pozivnice i registraciju) | visok | M | novi paket internal/mail

 | 17 | Login varijanta B sa prijavom preko Telegrama | srednji | M | login.html, handlers_auth.go

 | 18 | Samostalna registracija + wizard + Turnstile | visok | L | nove rute, migracija (trial polja na tenant)

 | 19 | Trial, read-only režim, predračun, stranica Pretplata | visok | L | tenant, middleware, jobs

 | 20 | Upozorenje vozaču na pragu | srednji | S | jobs.go

### Neprovereno

- Prikaz CSV pregleda sa greškama i rezultat uvoza (screenshot samo praznog uvoza).

- Stvarni izgled poruka u Telegram klijentu (pregledano samo iz koda; bot token nije podešen lokalno).

- 500 stranica (iz koda: goli tekst „greška servera“).

- Tastaturna navigacija i čitač ekrana nisu testirani ručno; nalazi o labelama i fokusu su iz koda i CSS-a.

- Ponašanje na pravom telefonu sa slabim signalom; emulirano samo 390px u Chrome-u.

- Poreske i SEF obaveze za fakturisanje.



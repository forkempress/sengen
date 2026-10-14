> **NACRT — mora da pregleda pravnik pre upotrebe.** Placeholderi su u [ZAGRADAMA]. Struktura prati obavezne elemente iz čl. 45 st. 3 i 4 ZZPL („Sl. glasnik RS“ 87/2018).

# Ugovor o obradi podataka o ličnosti (Prilog 1 Uslova korišćenja)

**Rukovalac:** Firma koja koristi Dani90, podaci iz registracije: [NAZIV FIRME], [ADRESA], MB [MB], PIB [PIB].
**Obrađivač:** [IME I PREZIME], preduzetnik, [NAZIV RADNJE], [ADRESA], MB [MB], PIB [PIB], [EMAIL ZA PRIVATNOST].

Ugovor se zaključuje u elektronskom obliku prihvatanjem pri registraciji; Obrađivač čuva verziju, vreme i korisnika koji je prihvatio.

## 1. Predmet, priroda i svrha
Evidencija boravaka vozača Rukovaoca u Šengen zoni i proračun preostalih dana po pravilu 90/180, upozorenja dispečerima i podsetnici vozačima preko Telegram bota.

## 2. Trajanje
Dok traje korišćenje usluge, i do brisanja po tački 9.

## 3. Lica i vrste podataka
- **Vozači:** ime i prezime, oznaka vozila, Telegram chat ID, datumi ulaska i izlaska iz Šengena, tip boravka (posao/privatno), izvor unosa, napomena. [Ako se uključi: zemlja određena iz lokacije koju vozač pošalje, bez čuvanja koordinata.]
- **Korisnici Rukovaoca** (vlasnik, dispečeri): ime, email, uloga, Telegram chat ID, evidencija izmena.
- Posebne vrste podataka se ne obrađuju; Rukovalac ih ne unosi.

## 4. Obaveze Obrađivača
Obrađivač:
1. obrađuje podatke samo po pisanim uputstvima Rukovaoca (ovaj ugovor, podešavanja i radnje u aplikaciji), uključujući i u pogledu prenosa u druge države; ne koristi ih za svoje svrhe;
2. obezbeđuje da su sva lica koja imaju pristup obavezana na poverljivost;
3. primenjuje mere iz Priloga A;
4. angažuje podobrađivače samo prema tački 5;
5. pomaže Rukovaocu da odgovori na zahteve lica (pristup, ispravka, brisanje, prenosivost), kroz funkcije aplikacije ili u roku od [10] dana od zahteva;
6. pomaže Rukovaocu u vezi sa bezbednošću, povredom podataka, procenom uticaja i prethodnim mišljenjem Poverenika (čl. 50 i 52–55 ZZPL);
7. po prestanku briše ili vraća podatke prema tački 9;
8. stavlja na raspolaganje informacije potrebne za dokaz usklađenosti i omogućava razumnu kontrolu uz najavu od [30] dana, najviše jednom godišnje, o trošku Rukovaoca;
9. bez odlaganja upozorava Rukovaoca ako smatra da je uputstvo nezakonito.

## 5. Podobrađivači
Rukovalac daje opšte ovlašćenje za podobrađivače sa liste ispod. O nameri dodavanja ili zamene Obrađivač obaveštava emailom najmanje [15] dana unapred; Rukovalac može da se usprotivi i raskine ugovor.

| Podobrađivač | Uloga | Lokacija | Osnov prenosa |
|---|---|---|---|
| [Hetzner Online GmbH] | server, backup | [Nemačka/Finska] | čl. 64 ZZPL [pravnik] |
| Cloudflare, Inc. | DNS, zaštita saobraćaja | SAD/globalno | [čl. 65 ZZPL – pravnik] |
| Telegram | kanal poruka vozačima i dispečerima | [proveriti] | [pravnik] |

Rukovalac je upoznat da Telegram nije ugovorni obrađivač i da korišćenjem bota vozači i dispečeri prihvataju Telegramove uslove; Rukovalac odlučuje da li će koristiti bot.

## 6. Povreda podataka
Obrađivač obaveštava Rukovaoca bez nepotrebnog odlaganja, a najkasnije u roku od [48] časova od saznanja, sa opisom povrede, kategorija i približnog broja lica, verovatnih posledica i preduzetih mera, kako bi Rukovalac mogao da obavesti Poverenika u roku od 72 časa (čl. 52 ZZPL).

## 7. Mesto obrade
Primarni podaci i rezervne kopije čuvaju se u [EU]. Drugi prenosi samo prema tački 5.

## 8. Rokovi čuvanja
- deaktiviran vozač: brisanje posle [12] meseci, ili ranije na zahtev Rukovaoca;
- boravci: [2 godine] od datuma izlaska;
- evidencija izmena: [2 godine];
- rezervne kopije: rotacija [30] dana.

## 9. Po prestanku
Rukovalac može izvesti podatke u roku od [30] dana. Zatim Obrađivač briše sve podatke i kopije, a iz rezervnih kopija najkasnije za [30] dana, osim ako zakon traži čuvanje.

## 10. Odgovornost
Primenjuju se odredbe o odgovornosti iz Uslova korišćenja, osim kada zakon to ne dozvoljava [pravnik].

## Prilog A – Tehničke i organizacione mere
- HTTPS za sav saobraćaj; Telegram webhook zaštićen tajnim ključem.
- Lozinke kao bcrypt hash; sesije ističu posle 14 dana; zaštita od CSRF; ograničenje pokušaja prijave.
- Logičko razdvajanje podataka po firmi (`tenant_id` u svakom upitu, testovi izolacije).
- Uloge: vlasnik i dispečer; samo vlasnik upravlja korisnicima i podešavanjima.
- Pristup serveru: samo [IME], SSH ključem; [enkripcija diska/backup-a].
- Dnevne rezervne kopije [Litestream, lokacija]; provera obnove [kvartalno].
- Evidencija izmena (audit log).
- Logovi servera se čuvaju [30] dana.

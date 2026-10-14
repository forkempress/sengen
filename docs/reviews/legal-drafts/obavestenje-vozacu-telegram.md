> **NACRT — mora da pregleda pravnik pre upotrebe.** Placeholderi su u [ZAGRADAMA].

# Obaveštenje vozaču pri povezivanju sa botom

Gde: šalje se odmah posle uspešnog `/start <token>`, pre poruke „Povezani ste“ (`internal/telegram/bot.go:265`). `{FIRMA}` = `t.Name`. Ovo obaveštenje daje firma kao rukovalac (čl. 23 ZZPL); Dani90 samo isporučuje tekst.

## Tekst (do ~600 znakova, Telegram poruka)

```
ℹ️ Vaši podaci

{FIRMA} vodi evidenciju vaših dana u Šengenu (pravilo 90/180) da biste izbegli prekoračenje boravka.

Čuva se: ime, vozilo, datumi ulaska i izlaska iz Šengena (i za privatna putovanja, jer se i ona broje) i vaš Telegram nalog.
Vide: vlasnik i dispečeri {FIRMA}. Sistem tehnički održava Dani90.
Lokaciju ne pratimo.

Možete tražiti uvid, ispravku ili brisanje od {FIRMA}.
Više: [https://dani90.rs/privatnost-vozaci]
```

## Kad se uključi lokacija (zameniti red „Lokaciju ne pratimo.“)

```
📍 Lokaciju šaljete samo vi, dugmetom. Iz nje se beleži samo zemlja i datum, koordinate se ne čuvaju. Možete je isključiti bilo kad.
```

## Komanda /privatnost (predlog)
Isti tekst na zahtev, plus: „Da prekinete vezu sa botom, pošaljite /odjava.“ (komanda ne postoji, vidi LEG-04).

## Stranica /privatnost-vozaci (kratko, na sajtu)
Rukovalac je vaš poslodavac; svrha; podaci; ko vidi; obrađivač Dani90 [podaci]; Telegram kao kanal [država]; rok čuvanja [iz DPA]; prava (čl. 26–37 ZZPL) i kontakt firme; pravo na pritužbu Povereniku.

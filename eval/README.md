# Poređenje modela za čitanje skenova

Isti skenovi se čitaju sa Opus 5.5, Sonnet 5.5 i Haiku 5.5 (svaki dva puta), pa se pročitano poredi sa tačnim rezultatima koje upišeš ručno.

## Priprema

U `eval/dokumenti/` (ne ide u git) stavi 5–10 pravih skenova. Uz svaki ide CSV istog imena sa tačnim boravcima, u istom formatu kao uvoz istorije:

```
eval/dokumenti/
  sveska-petrovic.jpg
  sveska-petrovic.csv
  izvestaj-mart.pdf
  izvestaj-mart.csv
```

```
vozač,ulaz,izlaz,tip,napomena
Marko Petrović,01.02.2026,10.02.2026,posao,
Ana Jović,03.04.2026,,privatno,
```

- Ime vozača napiši onako kako se zove u aplikaciji. Model dobija ta imena kao spisak postojećih vozača firme.
- Izlaz ostavi prazan ako je vozač još u Šengenu. Prazan tip znači posao.
- Napomena se ne ocenjuje.
- Ako firma u aplikaciji ima i vozače kojih nema na skenu, napiši ih sve u `<ime>.vozaci.txt`, po jedno ime u redu.

Najkorisniji su skenovi koji su teški: rukom pisane sveske, loše fotografije, godina koja se ne vidi, precrtani redovi, više strana.

## Pokretanje

```bash
ANTHROPIC_API_KEY=sk-ant-api03-... go run ./cmd/doceval
```

Za 10 skenova trošak je ispod $1. Rezultat se ispisuje kao tabela, a sve greške po skenu idu u `eval/rezultati/<datum>/izvestaj.md`. Ako se test prekine, pokreni ga ponovo sa `-out eval/rezultati/<datum>` i nastaviće gde je stao. Pozivi koji nisu uspeli ponoviće se.

Ostale opcije: `-models opus,haiku`, `-reps 3`, `-dir drugi/folder`.

## Kako se ocenjuje

Boravak je tačan samo ako se slažu vozač, ulaz, izlaz i tip.

- **pogrešno pročitano**: boravak postoji, ali je pogrešan datum ili tip (isti vozač, isti ulaz ili isti izlaz)
- **izmišljeno**: boravak koga nema na skenu
- **propušteno**: boravak sa skena koji model nije vratio
- **greške označene nesigurno**: koliko pogrešnih boravaka je model sam označio žutim u pregledu; tu greške korisnik verovatno uoči
- **lažno nesigurno**: tačni boravci koje je model ipak označio kao nesigurne

Sa 10 skenova razlika od jedne-dve greške između modela je slučajnost. Model vredi menjati tek ako jeftiniji model nema više grešaka koje nisu označene kao nesigurne.

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

## Skenovi koje su učitali korisnici

Aplikacija čuva svaki pročitan sken 30 dana. Kad je uvoz potvrđen, tačni boravci su oni koji su sada u aplikaciji za vozače iz tog uvoza, u periodu koji je uvoz pokrio, pa se računaju i ispravke na stranici vozača. Ništa ne moraš da upisuješ ručno.

Povuci kopiju baze sa servera (kao u glavnom README-u, odeljak „Proba izmena pre produkcije“) i pokreni:

```bash
ANTHROPIC_API_KEY=sk-ant-api03-... go run ./cmd/doceval -db dani90.db
```

Greška koju niko nije ispravio računa se kao tačna, pa je model koji je sken pročitao u aplikaciji u prednosti. Izveštaj uz svaki sken piše koji je to model. Ako se svi modeli slažu u grešci, uporedi taj sken sa originalom.

Model za jednu firmu menja se na serveru, bez restarta. `seed` se pravi i kopira kao i server:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/seed-linux ./cmd/seed && scp bin/seed-linux veljko@SERVER_IP:/tmp/seed
```

```bash
ssh veljko@SERVER_IP "sudo -u dani90 env DB_PATH=/var/lib/dani90/dani90.db /tmp/seed -slug <firma> -doc-model haiku"
```

`-doc-model default` vraća firmu na model servera (`DOC_READ_MODEL`, podrazumevano Sonnet 5.5).

## Kako se ocenjuje

Boravak je tačan samo ako se slažu vozač, ulaz, izlaz i tip.

- **pogrešno pročitano**: boravak postoji, ali je pogrešan datum ili tip (isti vozač, isti ulaz ili isti izlaz)
- **izmišljeno**: boravak koga nema na skenu
- **propušteno**: boravak sa skena koji model nije vratio
- **greške označene nesigurno**: koliko pogrešnih boravaka je model sam označio žutim u pregledu; tu greške korisnik verovatno uoči
- **lažno nesigurno**: tačni boravci koje je model ipak označio kao nesigurne

Sa 10 skenova razlika od jedne-dve greške između modela je slučajnost. Model vredi menjati tek ako jeftiniji model nema više grešaka koje nisu označene kao nesigurne.

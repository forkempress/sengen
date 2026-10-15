# Brisanje cele firme (ručno)

Aplikacija još nema dugme za brisanje firme. Do tada firmu briše operater,
ručno na serveru, po ovoj proceduri. Brisanje jednog vozača radi vlasnik
firme sam: stranica vozača → „Podaci vozača“ → „Trajno obriši vozača“.

## Kada

- Firma je raskinula ugovor i istekao je rok za izvoz podataka iz Uslova
  korišćenja (tačka 7, [POPUNITI: rok]).
- Firma pisano traži brisanje (email sa adrese vlasnika naloga).

Zahtev i datum brisanja zapisati van baze, na primer u evidenciju zahteva,
jer posle brisanja u bazi ne ostaje trag o firmi.

## Pre brisanja

1. Proveriti identitet: zahtev mora stići sa emaila vlasnika naloga te firme.
2. Ponuditi izvoz: vlasnik za svakog vozača preuzima CSV (stranica vozača →
   „Izvezi podatke (CSV)“). Sačekati potvrdu ili istek roka.
3. Pronaći ID firme (`<ID>` u nastavku):
   ```sql
   SELECT id, name, slug FROM tenants WHERE slug = '<oznaka>';
   ```
4. Napraviti kopiju baze pre izmene i čuvati je najviše 7 dana, šifrovanu:
   ```bash
   sqlite3 /var/lib/dani90/dani90.db ".backup /var/lib/dani90/pre-brisanja-<ID>.db"
   ```
5. Ako firma ima povezanu grupu za upozorenja, ukloniti bota iz grupe nije
   naša obaveza, ali zapis u `tenants.settings` nestaje sa firmom.

## Brisanje

Aplikacija može da radi; sve ide u jednoj transakciji. Redosled poštuje
strane ključeve (`stays.created_by`, `planned_stays.created_by` i
`tenants.terms_accepted_by` pokazuju na `users`).

```sql
PRAGMA foreign_keys = ON;
BEGIN;
DELETE FROM sessions             WHERE tenant_id = <ID>;
DELETE FROM telegram_link_tokens WHERE tenant_id = <ID>;
DELETE FROM telegram_alert_log   WHERE tenant_id = <ID>;
DELETE FROM email_tokens         WHERE tenant_id = <ID>
   OR user_id IN (SELECT id FROM users WHERE tenant_id = <ID>);
DELETE FROM email_log            WHERE tenant_id = <ID>;
DELETE FROM audit_log            WHERE tenant_id = <ID>;
DELETE FROM planned_stays        WHERE tenant_id = <ID>;
DELETE FROM stays                WHERE tenant_id = <ID>;
DELETE FROM events               WHERE tenant_id = <ID>;
DELETE FROM stays_cross_tenant   WHERE tenant_id = <ID>
   OR driver_id IN (SELECT id FROM drivers WHERE tenant_id = <ID>);
DELETE FROM drivers              WHERE tenant_id = <ID>;
UPDATE tenants SET terms_accepted_by = NULL WHERE id = <ID>;
DELETE FROM users                WHERE tenant_id = <ID>;
DELETE FROM superadmin_notifications
 WHERE kind IN ('new_tenant', 'first_activity') AND ref = '<ID>'
    OR kind = 'trial_expiring' AND ref LIKE '<ID>:%';
DELETE FROM tenants              WHERE id = <ID>;
COMMIT;
```

## Posle brisanja

1. Provera — svi brojevi moraju biti 0:
   ```sql
   SELECT (SELECT COUNT(*) FROM tenants WHERE id = <ID>),
          (SELECT COUNT(*) FROM users WHERE tenant_id = <ID>),
          (SELECT COUNT(*) FROM drivers WHERE tenant_id = <ID>),
          (SELECT COUNT(*) FROM stays WHERE tenant_id = <ID>),
          (SELECT COUNT(*) FROM planned_stays WHERE tenant_id = <ID>),
          (SELECT COUNT(*) FROM audit_log WHERE tenant_id = <ID>);
   PRAGMA foreign_key_check;
   ```
2. Obrisati kopiju iz koraka 4 kad prođe 7 dana bez problema.
3. Rezervne kopije (Cloudflare R2): podaci firme nestaju iz njih tek kad
   kopije isteknu ([POPUNITI: rok čuvanja backupa]). Ne vraćati staru kopiju
   bez ponovnog brisanja ove firme.
4. Tehnički logovi servera mogu i dalje sadržati Telegram chat ID-jeve
   vozača i korisnika do isteka roka čuvanja logova ([POPUNITI]).
5. Fakture i podaci o naplati čuvaju se van ove baze, onoliko koliko propisi
   nalažu.
6. Odgovoriti podnosiocu zahteva da je brisanje izvršeno, sa datumom.

`superadmin_audit_log` ostaje: sadrži Telegram ID operatera, komande i
njihove argumente, a argument može biti oznaka ili ID obrisane firme. Ako
treba ukloniti i to:
`DELETE FROM superadmin_audit_log WHERE args LIKE '%<oznaka>%';`

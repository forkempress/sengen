-- Flash messages ("Vozač dodat.", "Uvoz neuspešan: …") used to travel in the
-- redirect URL as ?ok= and ?err=, so anyone could send a link that made the
-- app display their own text. They now live on the session row, are set by
-- the handler that redirects and cleared by the next page that shows them.
ALTER TABLE sessions ADD COLUMN flash_ok TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN flash_err TEXT NOT NULL DEFAULT '';

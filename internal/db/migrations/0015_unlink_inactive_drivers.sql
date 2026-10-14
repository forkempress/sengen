-- Deactivating a driver now also unlinks his Telegram account. Do the same
-- for drivers deactivated before that, so they lose bot access and their
-- accounts can be linked again.
UPDATE drivers SET telegram_chat_id = NULL WHERE active = 0;

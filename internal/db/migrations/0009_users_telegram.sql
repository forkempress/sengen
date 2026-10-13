ALTER TABLE users ADD COLUMN telegram_chat_id INTEGER;

CREATE UNIQUE INDEX idx_users_telegram_chat ON users(telegram_chat_id) WHERE telegram_chat_id IS NOT NULL;

-- Schema v7 (feature 007): the web UI has no login. The owner password hash and the login
-- sessions are deleted.
DELETE FROM settings WHERE key = 'owner_password_hash';
DROP TABLE sessions;

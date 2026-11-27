-- Preserve existing account identifiers, password hashes, sessions and budgets.
-- Existing email-shaped identifiers become ordinary logins without data changes.
ALTER TABLE specialists RENAME COLUMN email TO login;
ALTER TABLE specialists RENAME CONSTRAINT specialists_email_key TO specialists_login_key;
ALTER TABLE specialists RENAME CONSTRAINT specialists_email_check TO specialists_login_check;

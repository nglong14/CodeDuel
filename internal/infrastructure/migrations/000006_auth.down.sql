BEGIN;

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_display_name_check,
    DROP CONSTRAINT IF EXISTS users_email_normalized_check,
    ALTER COLUMN email DROP NOT NULL;

ALTER TABLE users
    DROP COLUMN IF EXISTS display_name;

COMMIT;

BEGIN;

ALTER TABLE users
    ADD COLUMN display_name TEXT;

DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM users WHERE email IS NULL) THEN
        RAISE EXCEPTION '000006_auth cannot migrate users with NULL email'
            USING HINT = 'Reset the disposable development database or assign every user a unique valid email.';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM users
        GROUP BY lower(btrim(email))
        HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION '000006_auth found emails that collide after normalization'
            USING HINT = 'Reset the disposable development database or resolve case and whitespace collisions.';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM users
        WHERE octet_length(lower(btrim(email))) NOT BETWEEN 3 AND 254
           OR char_length(split_part(lower(btrim(email)), '@', 1)) NOT BETWEEN 1 AND 64
           OR split_part(lower(btrim(email)), '@', 1) <> btrim(split_part(lower(btrim(email)), '@', 1))
    ) THEN
        RAISE EXCEPTION '000006_auth found an email that cannot produce a valid display name'
            USING HINT = 'Reset the disposable development database or correct the affected email.';
    END IF;
END
$migration$;

UPDATE users
SET email = lower(btrim(email)),
    display_name = split_part(lower(btrim(email)), '@', 1);

ALTER TABLE users
    ALTER COLUMN email SET NOT NULL,
    ALTER COLUMN display_name SET NOT NULL,
    ADD CONSTRAINT users_email_normalized_check CHECK (
        email = lower(email)
        AND email = btrim(email)
        AND octet_length(email) BETWEEN 3 AND 254
    ),
    ADD CONSTRAINT users_display_name_check CHECK (
        display_name = btrim(display_name)
        AND char_length(display_name) BETWEEN 1 AND 64
    );

COMMIT;

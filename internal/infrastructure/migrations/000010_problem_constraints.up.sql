ALTER TABLE problems
    ADD COLUMN constraints TEXT NOT NULL DEFAULT '';

UPDATE problems
SET constraints = 'Input contains at least two integers and exactly one valid pair.'
WHERE title = 'Two Sum';

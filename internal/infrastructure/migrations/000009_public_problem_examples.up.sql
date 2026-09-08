ALTER TABLE problems
    ADD COLUMN public_examples JSONB NOT NULL DEFAULT '[]'::jsonb;

UPDATE problems
SET public_examples = '[
    {
        "input": "4 9\n2 7 11 15",
        "output": "0 1"
    }
]'::jsonb
WHERE title = 'Two Sum';

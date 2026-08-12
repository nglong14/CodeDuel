INSERT INTO problems (title, statement, test_cases)
VALUES (
    'Two Sum',
    E'Given an array of integers nums and an integer target, return indices of the two numbers such that they add up to target.\n\nYou may assume that each input has exactly one solution, and you may not use the same element twice.\n\nInput format (stdin):\n- Line 1: n target\n- Line 2: n space-separated integers\n\nOutput format (stdout):\n- Two space-separated indices (0-based), in ascending order.',
    '[
        {"input": "4 9\n2 7 11 15", "expected": "0 1"},
        {"input": "3 6\n3 2 4", "expected": "1 2"},
        {"input": "2 6\n3 3", "expected": "0 1"}
    ]'::jsonb
);

# Catalog Change Log

Changes to the exercise catalog and logged history that were made as **data
edits on the live database**, not through `pgMigrations`. Schema changes live
in `internal/db/postgres.go`. This file is the Git record of everything else:
renames, merges, and deletions of catalog rows, and re-pointing of logged sets.

Each entry gives the structural SQL as it was applied, in one transaction, and
the check that ran afterwards. Rewrites of free text (exercise descriptions,
plan rationales) are summarized, not reproduced.

## 2026-11-26: unilateral/bilateral cleanup

**Convention adopted** (see README, *Exercise catalog*): every exercise is
bilateral by default. A unilateral exercise is one catalog row with
`unilateral = true`, and its name starts with "Unilateral". It is never a
left/right pair. Only *unilateral* / *bilateral* describe one side vs. both.

**Problem.** An earlier left/right split left behind duplicate catalog rows
(two "…, Right" rows with no logged sets, and an older single-arm lat pulldown
row with one day of history). The canonical single-arm lat pulldown was named
"Lat Pulldown (Neutral Grip)", which reads as the bilateral bar machine with a
neutral attachment.

**Merge the duplicate single-arm lat pulldown (159 → 162), then delete the
invalid rows (159, 163, 165):**

```sql
BEGIN;
UPDATE sets SET exercise_id = 162, name = 'Lat Pulldown (Neutral Grip)',
       color = (SELECT color FROM exercises WHERE id = 162)
 WHERE exercise_id = 159;                                   -- 3 sets, 2026-11-01
INSERT INTO exercise_muscles (exercise_id, muscle_id, is_primary)
  SELECT 162, muscle_id, is_primary FROM exercise_muscles WHERE exercise_id = 159
  ON CONFLICT DO NOTHING;                                   -- 2 mappings
DELETE FROM prescribed_items WHERE id IN (192, 194);        -- "Right" half of a completed session
DELETE FROM exercises WHERE id IN (159, 163, 165);          -- guarded: no sets / plan refs remain
COMMIT;
```

**Put the "Unilateral" prefix on the names, and sync the logged set names:**

```sql
BEGIN;
UPDATE exercises SET name = 'Unilateral Lat Pulldown (Neutral Grip)' WHERE id = 162 AND unilateral;
UPDATE exercises SET name = 'Unilateral Dumbbell Bent-Over Row'      WHERE id = 164 AND unilateral;
UPDATE sets s SET name = e.name FROM exercises e
 WHERE s.exercise_id = e.id AND e.id IN (162, 164) AND s.name <> e.name;  -- 9 sets
COMMIT;
```

**Text rewrites (summarized):** the descriptions of exercises 47, 48, 160, 162
and 164, ten plan rationales, and one program's notes. These texts referred to
left/right exercise rows or used "isolated" / "isolateral". They now say that
a unilateral exercise is one row with the weaker side logged. Uses of
"isolation" that mean single-joint work were left as they are.

**Not history loss:** three sets on 2026-11-26 that were logged in error (the
exercise wasn't performed) were deleted at the user's request.

**Verified afterwards:**

```sql
-- every set points at an existing exercise and carries its current name
SELECT count(*) FILTER (WHERE exercise_id IS NULL) AS null_id,
       count(*) FILTER (WHERE s.name <> e.name)    AS name_mismatch
  FROM sets s LEFT JOIN exercises e ON e.id = s.exercise_id;   -- 0, 0
-- no remaining isolated/isolateral wording in catalog or plan text
```

Result: lat pulldowns are exactly two rows, *Lat Pulldown* (47, bilateral) and
*Unilateral Lat Pulldown (Neutral Grip)* (162). No logged day was lost.

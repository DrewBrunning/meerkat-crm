-- Rollback of 000055 (issue #928). The unique index is pure derived constraint
-- state; dropping it reverts to the pre-#928 behavior where a duplicate create
-- is accepted. The duplicate rows this migration removed cannot be restored —
-- their redundancy carried no unique information — so a rollback leaves the
-- data deduplicated.
DROP INDEX IF EXISTS idx_relationship_edges_natural_key;

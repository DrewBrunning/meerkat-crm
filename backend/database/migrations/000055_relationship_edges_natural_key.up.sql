-- Natural-key uniqueness for relationship_edges (issue #928).
--
-- ADR 0009 models a relationship edge as natural-keyed, and every sibling join
-- table already enforces its natural key (circle_members
-- UNIQUE(circle_id, member_vcard_uid), contact_sync_links
-- UNIQUE(subscription_id, href), calendar_event_links UNIQUE(subscription_id,
-- uid)). relationship_edges was the outlier: CreateRelationshipEdge did a bare
-- tx.Create with no duplicate lookup, so a double-click or two concurrent
-- POSTs inserted two confirmed rows for the same
-- (user_id, source_id, target_id, type). A client re-pull then rendered the
-- relationship twice and graph traversal double-counted.
--
-- Existing databases may already hold duplicates, so the dedup below runs
-- BEFORE the index is created (the CREATE UNIQUE INDEX would otherwise abort).
-- One row per natural key survives, chosen by the same authority order
-- services.pickEdgeToDrop applies during a contact merge: higher confidence,
-- then confirmed over suggested, then earlier created_at, then the smaller
-- UUID. The survivors' data (metadata, sensitivity, provenance) is untouched;
-- only the redundant duplicate rows are removed, and a database that never had
-- duplicates is unchanged (the DELETE matches nothing).
DELETE FROM relationship_edges
WHERE id IN (
    SELECT id FROM (
        SELECT id,
               ROW_NUMBER() OVER (
                   PARTITION BY user_id, source_id, target_id, type
                   ORDER BY confidence DESC,
                            (status = 'confirmed') DESC,
                            created_at ASC,
                            id ASC
               ) AS rn
        FROM relationship_edges
    )
    WHERE rn > 1
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_relationship_edges_natural_key
    ON relationship_edges(user_id, source_id, target_id, type);

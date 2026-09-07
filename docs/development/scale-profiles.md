# Representative benchmark dataset profiles (PERF-01)

This is the written-down definition of the **representative** datasets every
performance ticket in milestone `v0.6.9` runs against, and the resource
requirements an operator can size a deployment from (issue #468). "Representative"
is the load-bearing word: a benchmark over a hundred flat contacts measures
nothing that matters here — the operations that get slow are relationship
traversal on a dense graph, FTS over a large corpus, and export over contacts
with many repeatable fields, none of which exist in a thin dataset.

There is **one generator**, not three. Each profile is the hand-authored
canonical TEST-02 pathological manifest (`testdata/canonical-fixture/manifest.json`,
issue #430) block-replicated by `internal/largedata` and then given a user
count and a non-uniform graph. Anything the canonical dataset exercises — every
pathological record, the `RecordForContact` trap, the soft-delete + vcard-uid
recreate pair, the ~1700-char note, the Unicode and duplicate-detection data,
the sensitive rows — the profile datasets exercise with more rows, more users,
and a realistically lopsided relationship graph.

Consumers: #469 (PERF-02, core-operation benchmarks), #470 (PERF-03,
data-movement benchmarks), #471 (PERF-04, [budgets](perf-budgets.md)), #495 (large-dataset
migration — the migration slice; see [scale-testing.md](scale-testing.md)),
#498 (capacity under constrained resources), #453 (BACKUP-01, restore at
scale). Each consumes `internal/largedata.Profile` / `largedata.Populate`, not
its own fixture — the shared generator is asserted *by use*
(`internal/largedata/profiles_test.go`, `cmd/migratebench/main_test.go`), not
by intent.

## The profiles

`internal/largedata` defines four (`largedata.Profiles()`):

| Profile | Contacts / user | Users | Total contacts | Hub contacts | Hub fan-out | Chain depth | Rationale |
|---|--:|--:|--:|--:|--:|--:|---|
| `smoke`   |     162 |  2 |       324 |  2 |   8 |  6 | Tiny — exercises the graph shape, multi-user split and every pathological record fast enough to run in the default `go test` suite on every PR. Not a resource-sizing profile. |
| `typical` |     918 |  1 |       918 |  5 |  25 | 12 | A real personal-CRM user after a few years: a few hundred contacts, one account, a handful of well-connected people. |
| `large`   |  15,012 |  3 |    45,036 | 25 | 150 | 40 | A heavy user, or someone who imported an entire address-book history, on a small shared instance. |
| `stress`  | 100,008 | 10 | 1,000,080 | 60 | 400 | 80 | Beyond the intended MVP scale. Its job is to find the cliff, not to promise support. |

**Scaling unit.** The per-user contact count rounds up to a whole number of
27-contact manifest blocks (the canonical manifest carries 27 records since the
I18N-01 international records landed, issue #484), so the pathological records
are present at every scale rather than trimmed off by an off-by-one. Every
block is a faithful copy of the whole manifest with names, card UIDs and
cross-references re-keyed. The target counts above are deliberately whole
blocks, so the round-up is exact and the populated contact count equals
`Contacts × Users`.

**The graph is not uniform.** Block replication alone yields a disconnected
forest of identical 27-contact islands; traversal cost, though, lives in the
tails. Each profile adds, on top of the per-block edges:

- **one deep chain** — the lead contact of block *i* is `parent_of` the lead
  contact of block *i+1*, for `ChainDepth` hops. This is the multi-hop
  traversal path #469 measures.
- **dense hubs** — `Hubs` lead contacts spread evenly across the dataset, each
  `friend_of` the next `HubFanout` blocks' lead contacts. A hub's out-degree
  is far above the block average — that contact is a real user's real
  over-connected friend.

Both relation types are registry tokens and both endpoints are live lead
contacts, so the shaped dataset passes the DB-01 application-invariant sweep
(`services.RunDataIntegrityChecks`, issue #460) — asserted in
`TestProfileDatasetPassesDB01`. A fixture that violated an invariant would make
every downstream measurement meaningless.

**Multi-user is its own axis.** Multi-user is supported at `1.0.0` (#558). Each
profile user is a fully isolated account — its own re-keyed contacts, edges and
content, no cross-user references (`TestProfileMultiUserIsolation`). Per-user
background jobs, FTS indexes and integration sync schedules multiply with the
user count independently of per-user data volume: ten users each syncing
CardDAV on the same cadence is a contention profile one user with ten times the
data never produces (#498). The in-process scheduler is a single `gocron`
instance sharing one SQLite writer.

## Row shape (per profile)

Populated counts, measured. Per-block ratios are the canonical manifest's own,
minus the two relationship edges per block that each block's soft-deleted
`gina` hard-deletes on cascade (exactly as `DeleteContact` does).

| Entity | per block | `typical` (34 blk) | `large` (3×556 blk) | `stress` (10×3,704 blk) |
|---|--:|--:|--:|--:|
| contacts               | 27 |   918 | 45,036 | 1,000,080 |
| relationship edges†    |  ~8 + graph |   409 | 24,714 | ~537,000 |
| notes                  |  6 |   204 | 10,008 |   222,240 |
| life events            |  9 |   306 | 15,012 |   333,360 |
| gifts                  |  5 |   170 |  8,340 |   185,200 |
| activities             |  4 |   136 |  6,672 |   148,160 |
| attachments (metadata) |  4 |   136 |  6,672 |   148,160 |
| preferences            |  5 |   170 |  8,340 |   185,200 |
| external identities    |  3 |   102 |  5,004 |   111,120 |
| custom-field values    |  7 |   238 | 11,676 |   259,280 |
| households / circles / tags | 2 / 2 / 2 | 68 each | 3,336 each | 74,080 each |
| **soft-deleted contacts** | 1 |  34 |  1,668 |    37,040 |
| **vcard-uid-recreating contacts** | 1 | 34 | 1,668 | 37,040 |
| **~1,700-char notes** | 1 |  34 |  1,668 |    37,040 |

† `large`/`stress` edge totals include the deep chain (`ChainDepth` edges/user)
and the hubs (`Hubs × HubFanout` edges/user). `stress` is projected from the
per-block ratio and the graph formula, not measured — a `stress` populate is
~1M contacts × 10 users and is not run in CI. `typical`'s numbers are measured
by the gated `TestProfilePopulatesAtScale`.

## Storage

The fixture stores **attachment metadata only** — no file bytes have a home in
a reviewable dataset. The `size_bytes` a manifest block declares total ~767 KB
across 4 rows (a 240 KB PDF, a 500 KB JPEG, an 8 KB PDF, a 1 KB text file), so
the *declared* attachment volume is:

| Profile | Attachment rows | Declared bytes | Note-text corpus |
|---|--:|--:|--:|
| `typical` |     136 |  ~26 MB   | ~71 KB |
| `large`   |   6,672 |  ~1.3 GB  | ~3.5 MB |
| `stress`  | 148,160 |  ~28 GB   | ~77 MB |

`#470` (backup/restore) and `#453` (restore at scale) need real bytes on disk:
they write placeholder files sized to each row's `size_bytes` into
`ATTACHMENTS_DIR` before measuring, and must budget the table above as
out-of-`.db` storage that a `.db`-only backup silently loses
(`docs/deployment.md`).

## Measured resource requirements

Measured on a 16-core / 30 GB Linux x86_64 dev machine, Go 1.26, SQLite via the
glebarez/modernc pure-Go driver. Re-measure before quoting a number in a
release note; the generation contract below is the reproducibility guarantee.

| Profile | Total contacts | Generate (populate) | Final `.db` size | Notes |
|---|--:|--:|--:|---|
| `smoke`   |       324 | <1 s   | ~2 MB   | Per-PR suite. |
| `typical` |       918 | ~1 s (~5 s under `-race`) | ~1.2 MB | Gated `TestProfilePopulatesAtScale`. |
| `large`   |    45,036 | ~2 min | ~210 MB | Nightly / main. Cached as a CI artifact. |
| `stress`  | 1,000,080 | projected ~45 min | projected ~5 GB | Not run in CI. |

**Operator sizing.** A deployment should size its disk for
`final .db size + peak additional disk during the heaviest operation`, not the
final size alone. The peak-additional-disk figures for the migration path are
in [scale-testing.md](scale-testing.md) ("Recorded resource requirements"): a
`VACUUM INTO` backup needs room for a full second copy of the `.db`, and a
migration that rebuilds a table needs room for a copy of that table. Add the
declared attachment volume above as separate `ATTACHMENTS_DIR` storage.

**When a limit is exceeded** — what each operation does when disk, memory or
CPU actually runs out (completes / degrades with a clear error / fails closed /
never corrupts), the free-space multiples backup and migration require, and the
RSS floor per profile — is the `constraint × operation` table in
[capacity-under-constraint.md](capacity-under-constraint.md) (issue #498).

## Generating a profile

```bash
cd backend
go run ./cmd/migratebench seed --profile large --db /tmp/large.db
```

`migratebench seed` accepts either `--profile <name>` (this catalogue,
multi-user + graph shape) or `--contacts N` (the migration test's one-user,
graph-free, exact-row-count artifact). Both populate through
`canonicalfixture.Populate` → `models.ApplyRecordToContact`, i.e. a real
migrated schema and the same code paths the REST API uses (CLAUDE.md backend
traps #1/#2).

In Go:

```go
base, _ := canonicalfixture.Read()
ds, _ := largedata.Populate(db, base, largedata.Large) // db MUST be a real migrated schema
// ds.Users[i] is one canonicalfixture.Dataset per user
```

## Determinism

Generation is fully deterministic and **seeded**. Every regenerated contact UID
is derived from `(profile name, profile seed, user index, block, contact
index)`; the graph shape is pure index arithmetic, no RNG. The same
`(profile, seed)` produces byte-identical manifests, so two benchmark runs are
directly comparable and a regression reproduces against a specific seed rather
than a lucky dataset (`TestProfileUserManifestsDeterministic`). Bump
`Profile.Seed` to get a different but equally deterministic dataset.

## Where the tests run

| Test | Location | CI |
|---|---|---|
| Catalogue, determinism, manifest validity, graph shape, multi-user isolation, pathological-at-scale, DB-01 (all on `smoke`) | `internal/largedata/profiles_test.go` | rest leg, every PR |
| `migratebench seed --profile` wiring | `cmd/migratebench/main_test.go` | rest leg, every PR |
| `TestProfilePopulatesAtScale` (`typical`, row counts + DB-01 + integrity + size) | `internal/largedata/profiles_test.go` | migration-tests `large-dataset` job (main merge + nightly, `MYCORRHIZAL_LARGE_TESTS=1`) |
| `large` profile seed + integrity, cached | `.github/workflows/migration-tests.yml` | migration-tests `large-dataset` job |

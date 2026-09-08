package schemafixture

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"mycorrhizal/atrest"
	"mycorrhizal/config"
	"mycorrhizal/database"
	"mycorrhizal/models"
	"mycorrhizal/routes"
	"mycorrhizal/services"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// DEPLOY-02 (issue #451) — automated full-install upgrade testing.
//
// MIG-02 (mig02_test.go / upgrade_test.go) proves the migration chain runs
// against the release fixtures; MIG-03 (semantic_test.go) proves it preserves
// meaning; BACKUP-01 (cross_version_restore_test.go) proves a three-piece
// snapshot RESTORES across versions. DEPLOY-02 is the full-stack analogue: a
// real installed instance — the SQLite file PLUS PROFILE_PHOTO_DIR PLUS
// ATTACHMENTS_DIR, per docs/deployment.md — is upgraded IN PLACE by the next
// binary's startup migrations (database.InitDB, exactly as the server boots),
// and the whole install is validated, not the database alone:
//
//   - schema at current + clean + integrity ok, every table's row count and
//     the MIG-03 semantic content preserved (reusing this package's oracle);
//   - every non-deleted attachment/photo row still resolves to a real file
//     under the restored directories (a database-only check can pass while the
//     install is broken — issue #451 action 2);
//   - the mandatory pre-migration backup (#530) was actually taken DURING the
//     upgrade and is a valid, restorable rollback point at the PRE-upgrade
//     schema (issue #451 action 5);
//   - the migrated instance is exercised through the real HTTP stack: log in
//     with a PRE-EXISTING account (the bcrypt hash must survive the upgrade),
//     search (the FTS index must still be consistent), read + edit a contact,
//     and export (issue #451 action 3).
//
// The v0.6.0 subtest IS the longest supported skip (v0.6.0 -> current, issue
// #529 action 4), run through database.InitDB at once.
//
// Hand-verify (issue #451 "How to verify" / CLAUDE.md): a migration that drops
// a column without a backfill must fail the upgrade test naming the lost data.
// That is already wired by the semantic step below —
// assertContactsSemanticallyEqual + assertContentIdentical name the concept
// (email, adr, name.full, ...) and migration_expectation_test.go gates the
// declared-changeset files — so this suite inherits it rather than duplicating.

// deploy02LiveContact is a manifest contact that is live (not the soft-deleted
// gina) with an ASCII firstname, used for the search / read / edit / export
// assertions.
const deploy02LiveContact = "ada"

// deploy02FixturePassword is the plaintext password logged in with. The
// canonical manifest loader stores the users.password column VERBATIM (its
// own doc comment: "the password is never validated by the loader ... stored
// verbatim so the fixture user is a usable login for suites that hit the HTTP
// API") — but LoginUser compares it with bcrypt, so a verbatim value never
// validates. seedRealPasswordHash below replaces it with a real bcrypt hash
// pre-upgrade, so this suite proves the hash a real account carries INTO the
// upgrade still validates AFTER it.
const deploy02FixturePassword = "fixture-password-1!"

// seedRealPasswordHash overwrites the fixture user's password column with a
// real bcrypt hash of deploy02FixturePassword, called before the upgrade so
// the hash — not a placeholder — is what travels through the migration.
func seedRealPasswordHash(t *testing.T, f *Fixture) {
	t.Helper()
	hashed, err := services.HashPassword(deploy02FixturePassword)
	require.NoError(t, err)
	require.NoError(t, f.DB.Exec("UPDATE users SET password = ? WHERE id = ?", hashed, f.Dataset.User.ID).Error)
}

// TestFullInstallUpgrade upgrades a real three-piece install of each supported
// release in place and validates the whole install plus a live app workflow.
func TestFullInstallUpgrade(t *testing.T) {
	latest, err := database.LatestMigrationVersion()
	require.NoError(t, err)
	expectDir := committedMigrationsDir(t)

	for _, rel := range SupportedReleases {
		rel := rel
		name := rel.Tag
		if rel == SupportedReleases[0] {
			name = rel.Tag + " (longest skip -> current)"
		}
		t.Run(name, func(t *testing.T) {
			f := Load(t, rel)
			seedMigrationScopeData(t, f) // audit history + the etag/e_tag sync-state trap
			seedRealPasswordHash(t, f)
			pieces := seedFilePieces(t, f)
			assertCriteriaPopulated(t, f)

			beforeCounts := tableCounts(t, f.DB)
			before := withoutOperationalTables(captureContentSnapshot(t, f.DB))
			beforeContacts := contactRecords(t, f.DB, f.Dataset)
			username := f.Dataset.User.Username
			liveID := f.Dataset.Contacts[deploy02LiveContact].ID
			liveFirst := f.Dataset.Contacts[deploy02LiveContact].Firstname
			require.NotZero(t, liveID, "fixture must carry the %q contact", deploy02LiveContact)
			require.NotEmpty(t, liveFirst)
			closeFixtureDB(t, f.DB)

			// Lay out the three-piece install: a consistent copy of the
			// database beside real photo/attachment directories, the shape a
			// docker volume holds. BackupSnapshot (VACUUM INTO) is the same
			// primitive `make backup` uses and folds the WAL in, so the copy
			// is a complete image at the release's schema.
			base := t.TempDir()
			dbPath := filepath.Join(base, "mycorrhizal.db")
			require.NoError(t, database.BackupSnapshot(f.Path, dbPath))
			photoDir := filepath.Join(base, "photos")
			attachDir := filepath.Join(base, "attachments")
			copyTree(t, pieces.photoDir, photoDir)
			copyTree(t, pieces.attachDir, attachDir)
			// database.InitDB / MigrateUp write the mandatory pre-migration
			// backup (#530) to a `pre-migration` sibling of the database file
			// unless MYCORRHIZAL_PRE_MIGRATION_BACKUP_DIR overrides it.
			preMigrationDir := filepath.Join(base, "pre-migration")

			// The in-place upgrade: the exact path the next release's server
			// boots through. This also fires the #530 pre-migration backup.
			db, err := database.InitDB(dbPath)
			require.NoError(t, err, "the next binary's startup migrations must upgrade the %s install in place", rel.Tag)
			t.Cleanup(func() {
				if sqlDB, err := db.DB(); err == nil {
					_ = sqlDB.Close()
				}
			})

			version, dirty, ok, err := database.AppliedMigrationVersion(db)
			require.NoError(t, err)
			require.True(t, ok)
			assert.EqualValues(t, latest, version, "the %s install must land on the current schema", rel.Tag)
			assert.False(t, dirty, "the upgraded install must not be dirty")
			assertIntegrity(t, dbPath, "ok")
			assertRowCountsPreserved(t, rel.Tag+" full-install upgrade", beforeCounts, tableCounts(t, db))

			// MIG-03 semantic content: nothing changed unless a migration
			// declared it (same branch as TestCrossVersionRestoreOntoNewerRelease).
			after := withoutOperationalTables(captureContentSnapshot(t, db))
			afterContacts := contactRecords(t, db, f.Dataset)
			declared := loadExpectationFiles(t, expectDir, rel.Version, latest)
			if len(declared) == 0 {
				assertContentIdentical(t, rel.Tag, before, after)
				assertContactsSemanticallyEqual(t, rel.Tag, beforeContacts, afterContacts)
			} else {
				assertContentMatchesExpectations(t, rel.Tag, before, after, declared)
			}

			// The two out-of-database pieces: every live attachment/photo row
			// must still resolve to a real file after the in-place migrate.
			assertRestoreCompleteness(t, db, photoDir, attachDir)

			// The pre-migration backup is a real, restorable rollback point —
			// only taken when there was a real upgrade to back up (#530's
			// shouldTakePreMigrationBackup skips an install already at the
			// current schema, e.g. the newest supported release once it IS
			// current).
			if rel.Version < latest {
				assertPreMigrationBackupRestorable(t, preMigrationDir, dbPath, rel.Version, latest, photoDir, attachDir)
			}

			// Exercise the migrated instance through the real HTTP stack.
			exerciseUpgradedApp(t, db, username, liveID, liveFirst, photoDir, attachDir)

			// database.InitDB is only the migration half of boot. main.go then
			// runs the post-migration startup jobs — the at-rest encryption
			// backfill (#380), the Unicode NFC normalization backfill (#485),
			// and the audit hash-chain backfill (#381) — which rewrite real
			// pre-existing rows. A migration-only upgrade test cannot see one
			// of those jobs fail against real data: the at-rest backfill's
			// UPDATE of audit_events.before_snapshot (both encrypted at rest
			// AND behind the append-only immutability trigger) crash-looped
			// every used instance's upgrade until it learned to drop that
			// trigger around its writes. The NFC backfill (v0.6.11, migration
			// 000052) is the newest job of this shape — it rewrites contact
			// cards through the model save path, appending audit rows, with
			// encryption armed and the trigger live — so it belongs in the
			// same guard.
			verifyStartupBackfills(t, db)
		})
	}
}

// verifyStartupBackfills runs the post-migration startup jobs against an
// upgraded install in the exact order main.go runs them after database.InitDB
// (at-rest backfill first, so the audit chain hashes the decrypted
// before_snapshot; then the Unicode NFC backfill, which rewrites contact cards
// through the model save path with encryption already armed; then the audit
// hash-chain recompute, which links the audit rows the NFC saves appended),
// and asserts each succeeds against the fixture's real pre-existing rows —
// audit history is seeded by seedMigrationScopeData — that the encrypted-at-rest
// audit snapshot round-trips, that the append-only audit_events trigger is
// enforcing again afterwards, and that the hash chain verifies. It is the
// end-to-end regression guard for a startup backfill that collides with a
// table invariant.
func verifyStartupBackfills(t *testing.T, db *gorm.DB) {
	t.Helper()

	// Resolve the master key the way main.go does: atrest.EncryptionKey reads
	// JWT_SECRET_KEY when no dedicated key is configured.
	t.Setenv("JWT_SECRET_KEY", "deploy02-startup-jobs-jwt-secret-abcdefghijkl")
	kek, err := atrest.EncryptionKey()
	require.NoError(t, err)
	require.NotNil(t, kek, "a JWT secret must resolve an at-rest master key")
	require.NoError(t, atrest.Initialize(db, kek))
	t.Cleanup(atrest.ResetForTest)

	require.NoError(t, atrest.Backfill(db),
		"the at-rest backfill must encrypt pre-existing rows without tripping a table invariant")

	// Unicode NFC normalization backfill (#485, v0.6.11 / migration 000052).
	// Runs after at-rest encryption is armed (it reads and rewrites the
	// decrypted cards) and before the audit chain recompute (its model saves
	// append audit rows the recompute then links). This is the assertion the
	// suite was missing: the newest startup backfill of the crash-loop-prone
	// shape, exercised against real pre-existing contact rows with the
	// append-only audit_events trigger live.
	nfcStats, err := services.NormalizeContactRecordsToNFC(db)
	require.NoError(t, err,
		"the Unicode NFC backfill must rewrite pre-existing contact cards without tripping a table invariant")
	require.NotNil(t, nfcStats)
	require.False(t, nfcStats.AlreadyDone,
		"the data_backfills ledger must be empty on a fresh upgrade, so the backfill actually runs")
	require.Positive(t, nfcStats.ContactsScanned,
		"the upgraded fixture has contacts, so the backfill must scan at least one row")

	require.NoError(t, models.RecomputeAuditChain(db),
		"the audit hash-chain backfill must succeed after the at-rest and NFC backfills")

	// The seeded plaintext audit snapshot is now ciphertext at rest and
	// decrypts back through the serializer.
	var rawSnapshot string
	require.NoError(t, db.Table("audit_events").
		Where("before_snapshot IS NOT NULL AND before_snapshot <> ''").
		Order("id ASC").Limit(1).
		Pluck("before_snapshot", &rawSnapshot).Error)
	require.Contains(t, rawSnapshot, "encv1:",
		"audit_events.before_snapshot must be encrypted at rest after the backfill")

	var ev models.AuditEvent
	require.NoError(t, db.Where("before_snapshot LIKE ?", "encv1:%").
		Order("id ASC").First(&ev).Error)
	require.JSONEq(t, `{"firstname":"Bob"}`, ev.BeforeSnapshot,
		"the encrypted audit snapshot decrypts to the value seedMigrationScopeData wrote")

	var triggerCount int64
	require.NoError(t, db.Raw(
		"SELECT count(*) FROM sqlite_master WHERE type = 'trigger' AND name = 'audit_events_no_update'").
		Scan(&triggerCount).Error)
	require.EqualValues(t, 1, triggerCount,
		"the append-only trigger the backfill dropped must be restored")

	gaps, err := models.VerifyAuditChain(db)
	require.NoError(t, err)
	require.Empty(t, gaps, "the audit hash chain must verify after the startup backfills")

	// A second boot's jobs are a no-op and still leave the invariants intact.
	require.NoError(t, atrest.Backfill(db))
	secondNFC, err := services.NormalizeContactRecordsToNFC(db)
	require.NoError(t, err)
	require.True(t, secondNFC.AlreadyDone,
		"the data_backfills ledger short-circuits the NFC backfill on the second boot")
	require.NoError(t, models.RecomputeAuditChain(db))

	gaps, err = models.VerifyAuditChain(db)
	require.NoError(t, err)
	require.Empty(t, gaps, "the audit hash chain must still verify after a second boot's jobs")
}

// assertPreMigrationBackupRestorable is issue #451 action 5: the mandatory
// pre-migration snapshot (#530) must exist after the upgrade, be a valid
// database at the PRE-upgrade schema (so "install the previous release, restore
// this" actually works — downgrade being unsupported, this is the only way
// back), and its three-piece restore must resolve every file row.
func assertPreMigrationBackupRestorable(t *testing.T, dir, dbPath string, from, to uint, photoDir, attachDir string) {
	t.Helper()
	base := filepath.Base(dbPath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	pattern := filepath.Join(dir, fmt.Sprintf("%s-pre-migration-%d-to-%d-*.db", stem, from, to))
	matches, err := filepath.Glob(pattern)
	require.NoError(t, err)
	require.Lenf(t, matches, 1, "exactly one pre-migration snapshot must exist for the %d->%d hop (glob %s)", from, to, pattern)
	snap := matches[0]

	assertIntegrity(t, snap, "ok")
	version, dirty, ok, err := database.MigrationVersion(snap)
	require.NoError(t, err)
	require.True(t, ok, "the pre-migration snapshot must carry a schema_migrations row")
	assert.EqualValues(t, from, version, "the snapshot must capture the PRE-upgrade schema (a rollback point, not a post-migration copy)")
	assert.False(t, dirty)

	// Three-piece restore of the rollback point: the previous release's binary
	// opens it with no migration, and every attachment/photo row resolves.
	restoredDB, restoredPhotos, restoredAttach := restoreThreePieces(t, snap, photoDir, attachDir, false)
	rdb, err := database.OpenMigratedFile(restoredDB)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := rdb.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	rv, _, _, err := database.AppliedMigrationVersion(rdb)
	require.NoError(t, err)
	assert.EqualValues(t, from, rv, "restoring the rollback point under its own release must not migrate it")
	assertRestoreCompleteness(t, rdb, restoredPhotos, restoredAttach)
}

// exerciseUpgradedApp drives the migrated database through the real router
// (routes.RegisterRoutes, as main.go wires it) — issue #451 action 3: log in
// with a pre-existing account, search, read + edit a contact, export. Every
// call goes through GORM model scans, which is why the fixture's datetime
// columns must be a format GORM can scan (see data.go's literal()).
func exerciseUpgradedApp(t *testing.T, db *gorm.DB, username string, liveID uint, liveFirst, photoDir, attachDir string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		JWTSecretKey:     "deploy02-upgrade-suite-secret-key-32+chars",
		JWTExpiryHours:   24,
		ProfilePhotoDir:  photoDir,
		AttachmentsDir:   attachDir,
		FrontendURL:      "http://localhost:5173",
		Port:             "7300",
		ReminderTime:     "12:00",
		ReminderTimezone: "UTC",
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("cfg", *cfg)
		c.Next()
	})
	routes.RegisterRoutes(router, cfg, db, nil)

	call := func(method, path, token string, body []byte) *httptest.ResponseRecorder {
		var reader *strings.Reader
		if body != nil {
			reader = strings.NewReader(string(body))
		} else {
			reader = strings.NewReader("")
		}
		req := httptest.NewRequest(method, path, reader)
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	// 1. Log in with the PRE-EXISTING account — proves the bcrypt hash copied
	//    into the fixture and carried through every migration still validates.
	loginBody, _ := json.Marshal(map[string]string{"identifier": username, "password": deploy02FixturePassword})
	w := call(http.MethodPost, "/api/v1/login", "", loginBody)
	require.Equalf(t, http.StatusOK, w.Code, "pre-existing account must log in after the upgrade (body: %s)", w.Body.String())
	var token string
	for _, ck := range w.Result().Cookies() {
		if ck.Name == "auth_token" {
			token = ck.Value
		}
	}
	require.NotEmpty(t, token, "login must mint an auth_token cookie for the pre-existing account")

	// 2. Search — the FTS index must still resolve a pre-upgrade contact. A
	//    migration that silently invalidated the index passes every row-count
	//    check but fails here.
	w = call(http.MethodGet, "/api/v1/search?q="+url.QueryEscape(liveFirst), token, nil)
	require.Equalf(t, http.StatusOK, w.Code, "search must succeed after the upgrade (body: %s)", w.Body.String())
	assert.Containsf(t, w.Body.String(), liveFirst, "FTS search must find the pre-upgrade contact %q", liveFirst)

	// 3. Read + edit a contact — a real write path over migrated data.
	idPath := "/api/v1/contacts/" + strconv.FormatUint(uint64(liveID), 10)
	w = call(http.MethodGet, idPath, token, nil)
	require.Equalf(t, http.StatusOK, w.Code, "reading a pre-upgrade contact must succeed (body: %s)", w.Body.String())
	var edit models.ContactRecordInput
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &edit), "the GET response must round-trip into a ContactRecordInput")
	// Gender has two homes on ContactRecordInput (issue #515): the legacy flat
	// field and CRM.Gender. UpdateContact applies the flat field first, then
	// ApplyRecordToContact's reverse mapping re-derives it from CRM.Gender
	// whenever that is non-empty (models/contact_record_reverse.go) — the
	// manifest's "ada" already sets CRM.Gender, so that is the one that must
	// change for the edit to actually persist.
	edit.Gender = "other"
	edit.CRM.Gender = "other"
	editBody, err := json.Marshal(edit)
	require.NoError(t, err)
	w = call(http.MethodPut, idPath, token, editBody)
	require.Equalf(t, http.StatusOK, w.Code, "editing a pre-upgrade contact must succeed (body: %s)", w.Body.String())
	w = call(http.MethodGet, idPath, token, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var readBack struct {
		Gender string `json:"gender"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &readBack))
	assert.Equal(t, "other", readBack.Gender, "the edit must persist through the migrated schema")

	// 4. Export — exercises db.Find(&[]models.Contact{}) end to end.
	w = call(http.MethodGet, "/api/v1/export", token, nil)
	require.Equalf(t, http.StatusOK, w.Code, "export must succeed after the upgrade (body: %s)", w.Body.String())
	assert.NotEmpty(t, w.Body.String(), "export must return data")
	assert.Contains(t, w.Body.String(), liveFirst, "export must include a pre-upgrade contact")
}

// TestFullInstallUpgradeRefusesSubFloorDatabase is issue #451 action 6: a
// database below the #529 floor (v0.6.0 / migration 31) must REFUSE to upgrade
// with the two-step ErrSubFloorMigration naming v0.6.0 — never a best-effort
// single hop — and leave the database untouched. The database-package MIG-04
// tests (issues #439/#546) are the primary coverage; this pins it on the
// InitDB full-install path DEPLOY-02 owns.
func TestFullInstallUpgradeRefusesSubFloorDatabase(t *testing.T) {
	subFloor := database.SupportedUpgradeFloorVersion - 1
	path := filepath.Join(t.TempDir(), "mycorrhizal.db")
	require.NoError(t, database.MigrateUpTo(path, subFloor),
		"building a version-%d (sub-floor) database", subFloor)

	_, err := database.InitDB(path)
	require.Error(t, err, "a sub-floor database must refuse to migrate")
	var subErr *database.ErrSubFloorMigration
	require.Truef(t, errors.As(err, &subErr), "want *ErrSubFloorMigration, got %T: %v", err, err)
	assert.EqualValues(t, subFloor, subErr.Version)
	assert.Containsf(t, err.Error(), database.SupportedUpgradeFloorTag,
		"the refusal must name %s as the required intermediate", database.SupportedUpgradeFloorTag)

	version, dirty := migrationStateAtPath(t, path)
	assert.EqualValues(t, subFloor, version, "the refused database must be left untouched")
	assert.False(t, dirty)
}

// TestFullInstallUpgradeRefusesDirtyDatabase is the other half of issue #451
// action 6: a database left dirty by an interrupted migration must refuse to
// upgrade on top of the damage (issues #439/#546) rather than migrate.
func TestFullInstallUpgradeRefusesDirtyDatabase(t *testing.T) {
	f := Load(t, SupportedReleases[len(SupportedReleases)-1])
	require.NoError(t, f.DB.Exec("UPDATE schema_migrations SET dirty = 1").Error)
	closeFixtureDB(t, f.DB)

	_, err := database.InitDB(f.Path)
	require.Error(t, err, "a dirty database must refuse to migrate")
	var dirtyErr *database.ErrDirtyMigration
	require.Truef(t, errors.As(err, &dirtyErr), "want *ErrDirtyMigration, got %T: %v", err, err)

	version, dirty := migrationStateAtPath(t, f.Path)
	assert.EqualValues(t, f.Release.Version, version, "the refused database must be left untouched")
	assert.True(t, dirty, "the dirty flag must be left for the operator to resolve")
}

package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"testing"

	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// These are the issue #922 regression tests: a concurrent commit of the same
// keeper/loser pair must apply exactly once. Before the fix the pair was loaded
// *before* the merge transaction, so two overlapping commits could both observe
// the loser as live and the second re-applied against its stale snapshot —
// producing a second merge note and re-appended associations. CommitContactMerge
// now loads the pair inside the transaction, where the loser's tombstone is the
// compare-and-swap token.

// updateBarrier pauses the first UPDATE against `table` on this db until the
// test releases it. It is the write-side sibling of conditional_write_race_test.go's
// raceBarrier (issue #924), and uses the same CompareAndSwap rather than
// sync.Once so only the first update parks -- the racing request's own writes
// must sail through once the lock is released.
type updateBarrier struct {
	fired   atomic.Bool
	started chan struct{}
	release chan struct{}
}

func newUpdateBarrier(t *testing.T, db *gorm.DB, table string) *updateBarrier {
	t.Helper()
	b := &updateBarrier{started: make(chan struct{}), release: make(chan struct{})}
	name := "mergetest:pause-update-" + table
	db.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == table && b.fired.CompareAndSwap(false, true) {
			close(b.started)
			<-b.release
		}
	})
	t.Cleanup(func() {
		_ = db.Callback().Update().Remove(name)
	})
	return b
}

func (b *updateBarrier) waitForStart() { <-b.started }
func (b *updateBarrier) letGo()        { close(b.release) }

// postMerge drives CommitContactMerge without taking a *testing.T, so it is
// safe to call from a test goroutine.
func postMerge(router *gin.Engine, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(body)
	req, _ := http.NewRequest(http.MethodPost, "/contacts/merge", &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestContactMerge_ConcurrentCommit_AppliesExactlyOnce races two commits of the
// same pair. A is paused inside its transaction just before its keeper UPDATE,
// holding SQLite's write lock; B is started while A is parked. The second
// transaction must block and then re-load the loser as a tombstone, returning a
// clean 404 rather than re-applying a stale snapshot (which on the pre-fix code
// surfaced as a 500 revision conflict, or — before CON-01's CAS — a second
// merge note).
func TestContactMerge_ConcurrentCommit_AppliesExactlyOnce(t *testing.T) {
	db := dbtest.New(t)
	closeTestDBAtTeardown(t, db)

	user := models.User{Username: "merge-race", Password: "password123!A", Email: "merge-race@example.com"}
	require.NoError(t, db.Create(&user).Error)

	keeper := models.Contact{
		UserID: user.ID, Firstname: "Ada", Lastname: "Lovelace",
		Emails: []models.ContactEmail{{Type: "home", Value: "ada@example.com"}},
	}
	loser := models.Contact{
		UserID: user.ID, Firstname: "ADa", Lastname: "Lovelace",
		Emails: []models.ContactEmail{{Type: "work", Value: "ada.work@example.com"}},
	}
	require.NoError(t, db.Create(&keeper).Error)
	require.NoError(t, db.Create(&loser).Error)

	// One association on the loser the merge must re-point exactly once. A
	// duplicated application would leave two copies pointing at the keeper.
	require.NoError(t, db.Create(&models.Note{
		UserID: user.ID, ContactID: &loser.ID, Content: "repoint me once",
	}).Error)

	router := goldenMergeRouter(t, db, user.ID)
	barrier := newUpdateBarrier(t, db, "contacts")

	req := models.ContactMergeRequest{
		KeepID: keeper.ID, MergeID: loser.ID,
		Resolutions: map[string]string{"firstname": "Ada"},
	}

	var wg sync.WaitGroup
	var first, second *httptest.ResponseRecorder

	wg.Add(1)
	go func() {
		defer wg.Done()
		first = postMerge(router, req)
	}()

	// first has loaded the pair inside its transaction and is parked before
	// its keeper UPDATE, holding the write lock.
	barrier.waitForStart()

	wg.Add(1)
	go func() {
		defer wg.Done()
		second = postMerge(router, req)
	}()

	barrier.letGo()
	wg.Wait()

	codes := []int{first.Code, second.Code}
	sort.Ints(codes)
	require.Equal(t, []int{http.StatusOK, http.StatusNotFound}, codes,
		"exactly one commit must win and the other must be a clean 404 (loser tombstoned); got %d and %d", first.Code, second.Code)

	// Exactly one merge note, never two.
	var mergeNotes int64
	require.NoError(t, db.Model(&models.Note{}).
		Where("user_id = ? AND contact_id = ? AND content LIKE 'Merged contact #%'", user.ID, keeper.ID).
		Count(&mergeNotes).Error)
	assert.EqualValues(t, 1, mergeNotes, "a concurrent second commit must not write a duplicate merge note")

	// The loser's association was re-pointed exactly once (no re-appending).
	var repointed int64
	require.NoError(t, db.Model(&models.Note{}).
		Where("user_id = ? AND contact_id = ? AND content = ?", user.ID, keeper.ID, "repoint me once").
		Count(&repointed).Error)
	assert.EqualValues(t, 1, repointed, "the association must be re-pointed exactly once")

	// The loser is tombstoned exactly once (not resurrected and re-deleted).
	var live, unscoped int64
	require.NoError(t, db.Model(&models.Contact{}).Where("id = ?", loser.ID).Count(&live).Error)
	require.NoError(t, db.Unscoped().Model(&models.Contact{}).Where("id = ?", loser.ID).Count(&unscoped).Error)
	assert.Zero(t, live)
	assert.EqualValues(t, 1, unscoped)
}

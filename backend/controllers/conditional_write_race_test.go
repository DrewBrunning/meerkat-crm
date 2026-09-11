package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	apperrors "mycorrhizal/errors"
	"mycorrhizal/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// These are the CON-01 follow-up regression tests (issues #920, #924; ADR
// 0018) at the HTTP-handler layer, complementing the direct model-layer
// tests in models/revision_cas_test.go. Where those tests hand-verify
// bumpRevisionCAS itself, these prove the full production request path
// (UpdateContact/DeleteContact -> checkIfMatch -> Save() -> AfterSave ->
// handleRevisionConflict) behaves correctly when two requests genuinely
// race, exactly as issue #924's own "Fix" section asks for ("a
// goroutine-level test that races N concurrent PUTs").
//
// Both races are driven by a GORM query callback that pauses the first
// goroutine right after its load, before it writes -- deterministic (no
// sleeps, no retries) and immune to flakiness: the pause point is a real
// GORM lifecycle hook, not a timing guess.

// --- handleRevisionConflict (unit) -----------------------------------

func TestHandleRevisionConflict_Deleted_Is404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/contacts/1", nil)

	handled := handleRevisionConflict(c, "Contact", &models.ErrRevisionConflict{
		Entity: "Contact", ID: uint(1), ExpectedRevision: 3, Deleted: true,
	})
	require.True(t, handled)
	assert.Equal(t, http.StatusNotFound, w.Code)

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "NOT_FOUND", body.Error.Code)
}

func TestHandleRevisionConflict_StillLive_Is412(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/contacts/1", nil)

	handled := handleRevisionConflict(c, "Contact", &models.ErrRevisionConflict{
		Entity: "Contact", ID: uint(1), ExpectedRevision: 3, Deleted: false,
	})
	require.True(t, handled)
	assert.Equal(t, http.StatusPreconditionFailed, w.Code)

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				ExpectedRevision float64 `json:"expected_revision"`
			} `json:"details"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "PRECONDITION_FAILED", body.Error.Code)
	assert.EqualValues(t, 3, body.Error.Details.ExpectedRevision)
}

func TestHandleRevisionConflict_UnrelatedError_NotHandled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPut, "/contacts/1", nil)

	handled := handleRevisionConflict(c, "Contact", apperrors.ErrDatabase("boom").WithError(nil))
	assert.False(t, handled, "an unrelated error must fall through to the caller's own handling")
}

// --- raceBarrier: pause the first query against `table` on this db,
// releasing it only when the test says so. ------------------------------

type raceBarrier struct {
	fired   atomic.Bool
	started chan struct{}
	release chan struct{}
}

func newRaceBarrier(t *testing.T, db *gorm.DB, table string) *raceBarrier {
	t.Helper()
	b := &raceBarrier{started: make(chan struct{}), release: make(chan struct{})}
	name := "racetest:pause-" + table
	db.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
		// CompareAndSwap, not sync.Once: Once.Do blocks EVERY caller until the
		// first invocation's function returns, even the ones that won't run
		// it -- exactly the deadlock this barrier exists to avoid, since our
		// function deliberately doesn't return until the test says so, and
		// the second racing request's own query against this same table must
		// sail through immediately, not queue up behind the first.
		if tx.Statement.Table == table && b.fired.CompareAndSwap(false, true) {
			close(b.started)
			<-b.release
		}
	})
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove(name)
	})
	return b
}

func (b *raceBarrier) waitForStart() { <-b.started }
func (b *raceBarrier) letGo()        { close(b.release) }

// TestConditionalWrite_HTTPRace_LostUpdate is issue #924's exact request:
// two concurrent PUTs, both loading revision 1 before either writes.
// Sequentially replaying B-then-A (TestConditionalWrite_CanonicalLostUpdateSequence)
// cannot exercise this -- A's own load there already observes B's commit
// and checkIfMatch alone rejects it. This test forces the genuine race: A's
// load completes and is paused *before* B ever starts, so both requests are
// holding revision 1 in memory at the same time, exactly like two real
// concurrent clients.
func TestConditionalWrite_HTTPRace_LostUpdate(t *testing.T) {
	env := newCWEnv(t)
	id := strconv.Itoa(int(env.alice.ID))

	barrier := newRaceBarrier(t, env.db, "contacts")

	var wg sync.WaitGroup
	var wA *httptest.ResponseRecorder
	wg.Add(1)
	go func() {
		defer wg.Done()
		wA = env.do("PUT", "/contacts/"+id, `"1"`, contactCardBody("A-change", ""))
	}()

	barrier.waitForStart() // A has loaded revision 1 and is paused before its write.

	wB := env.do("PUT", "/contacts/"+id, `"1"`, contactCardBody("B-change", ""))
	require.Equal(t, http.StatusOK, wB.Code, wB.Body.String())

	barrier.letGo()
	wg.Wait()

	assert412(t, wA)

	var stored models.Contact
	require.NoError(t, env.db.First(&stored, env.alice.ID).Error)
	assert.Equal(t, "B-change", stored.Firstname, "B's committed write must survive a genuinely racing A")
	assert.EqualValues(t, 2, stored.Revision, "the revision must not have been double-bumped or reused by A")
}

// TestConditionalWrite_HTTPRace_ConcurrentDeleteRejectsStaleUpdate is issue
// #920's exact request replayed through the real HTTP handlers: A loads the
// contact and is paused before its write; a DELETE lands and commits in
// between; A resumes and must be rejected -- not resurrect the row with its
// stale field values.
func TestConditionalWrite_HTTPRace_ConcurrentDeleteRejectsStaleUpdate(t *testing.T) {
	env := newCWEnv(t)
	id := strconv.Itoa(int(env.alice.ID))

	barrier := newRaceBarrier(t, env.db, "contacts")

	var wg sync.WaitGroup
	var wA *httptest.ResponseRecorder
	wg.Add(1)
	go func() {
		defer wg.Done()
		wA = env.do("PUT", "/contacts/"+id, `"1"`, contactCardBody("A-change", ""))
	}()

	barrier.waitForStart() // A has loaded the (still live) contact and is paused before its write.

	wDel := env.do("DELETE", "/contacts/"+id, `"1"`, nil)
	require.Equal(t, http.StatusOK, wDel.Code, wDel.Body.String())

	barrier.letGo()
	wg.Wait()

	require.NotEqual(t, http.StatusOK, wA.Code, "a stale update racing a concurrent delete must not succeed")
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(wA.Body.Bytes(), &body))
	assert.Equal(t, "NOT_FOUND", body.Error.Code, "the row is gone -- this must surface as 404, not a silent resurrection")

	var reFetched models.Contact
	require.NoError(t, env.db.Unscoped().First(&reFetched, env.alice.ID).Error)
	assert.True(t, reFetched.DeletedAt.Valid, "the row must remain soft-deleted -- no resurrection")
	assert.Equal(t, "Alice", reFetched.Firstname, "A's stale edit must not have been persisted")
}

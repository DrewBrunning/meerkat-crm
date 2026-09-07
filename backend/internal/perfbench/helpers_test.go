package perfbench

import (
	"encoding/json"
	"testing"

	"mycorrhizal/internal/largedata"

	"github.com/stretchr/testify/assert"
)

func TestTruncate(t *testing.T) {
	assert.Equal(t, "abc", truncate([]byte("abc"), 5))
	assert.Equal(t, "abc", truncate([]byte("abc"), 3))
	assert.Equal(t, "ab...", truncate([]byte("abcdef"), 2))
}

func TestMustJSON(t *testing.T) {
	got := mustJSON(map[string]int{"a": 1})
	assert.JSONEq(t, `{"a":1}`, string(got))
}

func TestContactRecordInputJSON(t *testing.T) {
	for _, given := range []string{"Ada", ""} {
		var parsed struct {
			Card struct {
				Name struct {
					Components []struct {
						Kind, Value string
					}
				}
			}
		}
		require := assert.New(t)
		require.NoError(json.Unmarshal(contactRecordInputJSON(given, "Lovelace"), &parsed))
		require.Len(parsed.Card.Name.Components, 2)
		require.Equal("given", parsed.Card.Name.Components[0].Kind)
		require.NotEmpty(parsed.Card.Name.Components[0].Value) // "" falls back to "Perf"
		require.Equal("Lovelace", parsed.Card.Name.Components[1].Value)
	}
}

func TestHumanDuration(t *testing.T) {
	assert.Equal(t, "12ns", humanDuration(12))
	assert.Equal(t, "1.5µs", humanDuration(1500))
	assert.Equal(t, "2.0ms", humanDuration(2_000_000))
	assert.Equal(t, "3.00s", humanDuration(3_000_000_000))
}

func TestBackticked(t *testing.T) {
	assert.Equal(t, []string{"`a`", "`b`"}, backticked([]string{"a", "b"}))
	assert.Empty(t, backticked(nil))
}

func TestRound2(t *testing.T) {
	assert.Equal(t, 3.14, round2(3.14159))
	assert.Equal(t, 3.0, round2(2.999))
}

func TestRatioEdges(t *testing.T) {
	assert.Equal(t, 1.0, ratio(0, 0))
	assert.Equal(t, 5.0, ratio(0, 5))
	assert.Equal(t, 2.0, ratio(3, 6))
}

func TestSecondHubBlock(t *testing.T) {
	// Pure block math, given an arbitrary manifest length argument: with
	// smoke's 162-contacts-per-user target and a 15-contact manifest it is
	// 11 blocks, so 2 hubs land one at block 0 and one at block 5.
	assert.Equal(t, 5, secondHubBlock(largedata.Smoke, 15))
	// a one-hub / one-block shape falls back to the chain head (block 0).
	assert.Equal(t, 0, secondHubBlock(largedata.Profile{Contacts: 15, Hubs: 1}, 15))
	assert.Equal(t, 0, secondHubBlock(largedata.Profile{Contacts: 10, Hubs: 5}, 15))
}

func TestNewEnv_RequiresWorkDir(t *testing.T) {
	_, err := NewEnv(EnvOptions{Profile: largedata.Smoke})
	assert.ErrorContains(t, err, "WorkDir is required")
}

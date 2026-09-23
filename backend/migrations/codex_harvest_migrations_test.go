package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexHarvestMigrationsCreateLearningAndFlowStorage(t *testing.T) {
	nodes, err := FS.ReadFile("239_codex_harvest_node_learning.sql")
	require.NoError(t, err)
	require.Contains(t, string(nodes), "CREATE TABLE IF NOT EXISTS codex_harvest_nodes")
	require.Contains(t, string(nodes), "cooldown_until")
	require.Contains(t, string(nodes), "REFERENCES accounts(id) ON DELETE CASCADE")

	flow, err := FS.ReadFile("240_codex_harvest_flow_events.sql")
	require.NoError(t, err)
	require.Contains(t, string(flow), "CREATE TABLE IF NOT EXISTS codex_harvest_flow_events")
	require.Contains(t, string(flow), "event_id VARCHAR(64) NOT NULL UNIQUE")
}

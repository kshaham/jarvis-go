package main

import (
	"fmt"
)

// handleAgent dispatches the `cm agent <subcommand>` command group. It powers
// the Guardian recovery protocol: a watching agent inspects the roster
// (`cm agent health`) to spot stuck or exited peers, then asks the server to
// respawn one (`cm agent restart <name>`).
func handleAgent(args []string) {
	if len(args) < 1 {
		fatal("Usage: cm agent restart <name> | cm agent health")
	}
	switch args[0] {
	case "restart":
		handleAgentRestart(args[1:])
	case "health":
		handleAgentHealth(args[1:])
	default:
		fatal("Usage: cm agent restart <name> | cm agent health")
	}
}

// handleAgentRestart asks the server to respawn a peer by display name. The
// peer-restart endpoint resolves the name to a config and relaunches the
// agent, so the caller only needs the human-readable name shown in the roster.
func handleAgentRestart(args []string) {
	requireArgs(args, 1, "cm agent restart <name>")
	name := args[0]
	result := postBody("/api/office/agent/peer-restart", map[string]interface{}{
		"sessionId": sessionID,
		"name":      name,
	})
	fmt.Printf("Restart requested for %q.\n", name)
	printJSON(result)
}

// handleAgentHealth fetches the team roster and prints each peer's name,
// status, and last activity in a compact table. It reuses the same
// team-roster endpoint as `cm roster` so the Guardian has a single source of
// truth for who is alive. Fields that the server does not populate fall back
// to "-" rather than crashing, so the command keeps working as the roster
// payload gains health metadata.
func handleAgentHealth(_ []string) {
	result := get("/api/office/agent/team-roster")
	roster := extractRoster(result)
	if len(roster) == 0 {
		fmt.Println("No peers in roster.")
		return
	}
	fmt.Printf("%-24s  %-12s  %s\n", "NAME", "STATUS", "LAST ACTIVITY")
	for _, peer := range roster {
		fmt.Printf("%-24s  %-12s  %s\n",
			rosterField(peer, "name"),
			rosterField(peer, "status"),
			rosterLastActivity(peer))
	}
}

// extractRoster unwraps the {"roster": [...]} envelope returned by the
// team-roster endpoint into a slice of per-peer maps. It tolerates either a
// bare array or the wrapped object so it survives small response-shape shifts.
func extractRoster(result interface{}) []map[string]interface{} {
	var raw []interface{}
	switch v := result.(type) {
	case map[string]interface{}:
		if arr, ckOK := v["roster"].([]interface{}); ckOK {
			raw = arr
		}
	case []interface{}:
		raw = v
	}
	peers := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		if peer, isMap := item.(map[string]interface{}); isMap {
			peers = append(peers, peer)
		}
	}
	return peers
}

// rosterField returns a string value for a roster key, falling back to "-"
// when the key is absent or empty so the table stays aligned.
func rosterField(peer map[string]interface{}, key string) string {
	if v, present := peer[key]; present {
		if s, isStr := v.(string); isStr && s != "" {
			return s
		}
		if v != nil {
			return fmt.Sprintf("%v", v)
		}
	}
	return "-"
}

// rosterLastActivity reads whichever last-activity key the server exposes
// (the field name has varied across versions), defaulting to "-".
func rosterLastActivity(peer map[string]interface{}) string {
	for _, key := range []string{"lastActivity", "lastActivityAt", "lastSeen", "updatedAt"} {
		if v, present := peer[key]; present && v != nil {
			if s, isStr := v.(string); isStr {
				if s != "" {
					return s
				}
				continue
			}
			return fmt.Sprintf("%v", v)
		}
	}
	return "-"
}

package office

// Z.AI rate-limit-aware model allocation.
//
// Z.AI's subscription plan limits concurrent requests per model. When
// jarvis spawns a multi-agent team against Z.AI, naively assigning
// every agent to the most-capable model would have most of them
// instantly throttled. AllocateZAIModels distributes agents across
// the available models (highest-capability first, cascading down)
// based on per-model concurrent-request limits.
//
// Behaviour:
//
//  1. Each agent has a requested model (operator-set, from the spawn
//     modal). Empty / unknown requests default to glm-5.1 (the most
//     capable tier).
//  2. The allocator walks the downgrade chain starting from the
//     requested tier. The first tier with available slots gets the
//     agent.
//  3. If every tier is full, the agent falls back to glm-4.7 (one
//     more concurrent slot beyond the cap is acceptable for the
//     overflow case, since glm-4.7's limit is the largest of the
//     capable tiers).
//
// This used to live in internal/web/handler/context.go but the
// office package is the right home because it's pure spawn-side
// logic with no HTTP / handler concerns.

// ZAIRateLimits caps the concurrent requests per Z.AI model. Tuned
// to the published Z.AI subscription terms; bump when Z.AI publishes
// new limits.
var ZAIRateLimits = map[string]int{
	"glm-5.1":       2,
	"glm-5":         2,
	"glm-4.7":       3,
	"glm-4.7-flash": 5,
	"glm-4.5-air":   5,
}

// ZAIDowngradePath is the cascade order when the requested model's
// concurrent slots are full. Highest-capability first, cheapest
// last. Don't reorder casually — the allocator walks it linearly.
var ZAIDowngradePath = []string{"glm-5.1", "glm-5", "glm-4.7", "glm-4.7-flash", "glm-4.5-air"}

// zaiDefaultModel is the floor an agent sees when the operator
// didn't specify a model in the spawn modal.
const zaiDefaultModel = "glm-5.1"

// zaiOverflowFallback is the model assigned when every tier in
// ZAIDowngradePath is already saturated. Chosen as glm-4.7 because
// it has the largest rate-limit budget of the capable tiers, giving
// the overflow agent the best chance of actually completing turns.
const zaiOverflowFallback = "glm-4.7"

// defaultModelSlotLimit is the per-model concurrent-request floor
// applied when the model name is missing from ZAIRateLimits.
const defaultModelSlotLimit = 3

// AllocateZAIModels distributes models across agents to respect Z.AI's
// per-model concurrent-request limits. For each agent in memberIDs,
// it tries to honor the operator's requested model (looked up in
// requestedModels by member ID); if that model's concurrent slots
// are full, it cascades down ZAIDowngradePath to the next tier.
//
// Returns a map of memberID → allocated model. The map's value can
// then be passed to the per-member provider env builder so the
// spawned agent sees the right ANTHROPIC_DEFAULT_SONNET_MODEL.
//
// Pure function: no global state mutation, no Context access.
func AllocateZAIModels(requestedModels map[string]string, memberIDs []string) map[string]string {
	allocated := make(map[string]string, len(memberIDs))
	usage := make(map[string]int)

	for _, id := range memberIDs {
		requested := requestedModels[id]
		if requested == "" {
			requested = zaiDefaultModel
		}

		// Find the requested model's index in the downgrade chain.
		// Unknown requests start from the top of the chain so they
		// still benefit from rate-limit-aware allocation.
		startIdx := 0
		for i, m := range ZAIDowngradePath {
			if m == requested {
				startIdx = i
				break
			}
		}

		assigned := false
		for i := startIdx; i < len(ZAIDowngradePath); i++ {
			model := ZAIDowngradePath[i]
			limit := ZAIRateLimits[model]
			if limit == 0 {
				limit = defaultModelSlotLimit
			}
			if usage[model] < limit {
				allocated[id] = model
				usage[model]++
				assigned = true
				break
			}
		}
		if !assigned {
			allocated[id] = zaiOverflowFallback
			usage[zaiOverflowFallback]++
		}
	}

	return allocated
}

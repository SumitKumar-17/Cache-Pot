package graph

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/SumitKumar-17/cache-pot/internal/llm"
)

// extractSystemPrompt is the fixed system prompt used for every extraction
// request. It asks for an exact, machine-parseable JSON shape and spells
// out the id-derivation rule so a well-behaved real LLM produces the same
// entity id across separate calls that mention the same entity (e.g.
// "Redis" and "the Redis cache" should both become id "redis").
//
// IMPORTANT: internal/llm's mock CompletionProvider (see
// internal/llm/mock.go) is task-agnostic and does NOT follow this
// instruction -- it just echoes back a truncated, clearly-marked slice of
// the user prompt, which is never valid JSON in this shape. See Extract's
// doc comment for how that is handled: as a graceful "nothing extracted",
// not an error. This is the same mock-degradation posture
// internal/consolidate already established for its own summarization
// prompt (see internal/consolidate/consolidate.go's doc comment).
const extractSystemPrompt = `You are extracting a knowledge graph from a single piece of text. Identify notable entities (people, systems, projects, organizations, and concepts) mentioned in the text, and directed relationships between them.

Respond with ONLY valid JSON -- no prose, no markdown code fences, no explanation -- in exactly this shape:

{"entities":[{"id":"<id>","label":"<display name>"}],"relations":[{"from":"<entity id>","to":"<entity id>","label":"<relationship>"}]}

Entity ids must be short, stable, lowercase, and use underscores instead of spaces or punctuation (e.g. "redis", "project_a", "alice"), derived consistently from the entity's name so the same real-world entity produces the same id across separate calls. Every "from"/"to" value in "relations" must reference an id present in "entities". If there is nothing worth extracting, respond with {"entities":[],"relations":[]}.`

// extractionResponse is the exact JSON shape Extract asks the model to
// produce and parses the completion's response text as.
type extractionResponse struct {
	Entities  []extractedEntity   `json:"entities"`
	Relations []extractedRelation `json:"relations"`
}

type extractedEntity struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type extractedRelation struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label"`
}

// memoryNodeID builds the id of the provenance node representing a source
// memory, e.g. "memory:abc123". Exported-ish via this helper (rather than
// inlined at each call site) so RESP/MCP callers that want to look the
// memory-node back up via GetNode/Related use the exact same id shape
// Extract itself used.
func memoryNodeID(memoryID string) string {
	return "memory:" + memoryID
}

// Extract asks completion to extract entities and relationships from
// memoryContent, then records them into store under workspaceID: every
// extracted entity becomes a Node, every extracted relation becomes an
// Edge, and -- so the graph stays traceable back to the memory it came
// from, the original product vision's "Redis -> used by -> Project A ->
// maintained by -> Alice" framing rooted in real source content -- one more
// node representing the source memory itself (Node{ID: "memory:" +
// memoryID, Label: "memory"}) is added, with a "mentions" edge from that
// memory-node to each extracted entity. Returns how many entities and
// relations were added.
//
// Graceful mock-degradation (the most important behavior in this file): if
// completion's response isn't valid JSON in the expected shape --
// including empty/whitespace, prose, or anything else that fails
// json.Unmarshal -- this is treated as "nothing extracted": Extract returns
// (0, 0, nil), not an error. This is exactly what happens when completion
// is internal/llm's mock provider (see extractSystemPrompt's doc comment),
// and the graph store is left completely untouched in that case -- Extract
// never fabricates a graph from a response it can't actually parse. Only a
// genuine failure to call completion at all (a real error from
// completion.Complete, e.g. a network failure against a real provider) is
// reported as a non-nil error.
//
// If parsing succeeds but yields zero entities (a legitimate, well-formed
// {"entities":[],"relations":[]} response -- e.g. the model correctly
// decided there was nothing worth extracting), no memory-provenance node is
// added either: there would be nothing for it to "mention".
func Extract(ctx context.Context, completion llm.CompletionProvider, store *Store, workspaceID, memoryID, memoryContent string) (entitiesAdded, relationsAdded int, err error) {
	raw, _, err := completion.Complete(ctx, extractSystemPrompt, memoryContent)
	if err != nil {
		return 0, 0, fmt.Errorf("graph: complete: %w", err)
	}

	var parsed extractionResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		// Not valid JSON in the requested shape -- exactly what
		// internal/llm's mock provider produces. Graceful "nothing
		// extracted", not an error; see this function's doc comment.
		return 0, 0, nil
	}

	if len(parsed.Entities) == 0 {
		// Nothing to record, and nothing for a memory-provenance node to
		// mention -- see this function's doc comment. A well-formed
		// response with relations but no entities is nonsensical (every
		// relation must reference an entity id) and is treated the same
		// way: nothing usable was extracted.
		return 0, 0, nil
	}

	for _, e := range parsed.Entities {
		if e.ID == "" {
			continue
		}
		store.UpsertNode(workspaceID, Node{ID: e.ID, Label: e.Label})
		entitiesAdded++
	}

	for _, r := range parsed.Relations {
		if r.From == "" || r.To == "" {
			continue
		}
		store.UpsertEdge(workspaceID, Edge{FromID: r.From, ToID: r.To, Label: r.Label})
		relationsAdded++
	}

	if entitiesAdded == 0 {
		// Every entity in the response had an empty id (malformed model
		// output) -- nothing to mention, mirroring the zero-entities case
		// above.
		return 0, relationsAdded, nil
	}

	memNodeID := memoryNodeID(memoryID)
	store.UpsertNode(workspaceID, Node{ID: memNodeID, Label: "memory"})
	for _, e := range parsed.Entities {
		if e.ID == "" {
			continue
		}
		store.UpsertEdge(workspaceID, Edge{FromID: memNodeID, ToID: e.ID, Label: "mentions"})
	}

	return entitiesAdded, relationsAdded, nil
}

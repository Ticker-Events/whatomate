package codedflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// CatalogRevisionKey is the turn-context pointer to a catalog revision.
	CatalogRevisionKey = "catalog_revision_id"
	archiveSplitKey    = "_split"
	archivePartCount   = "part_count"
	archivePartData    = "data"
	archivePartIndex   = "part"
	v2StateKey         = "_v2_state"
)

var archiveCatalogKeys = []string{"collections", "products", "store", "options"}

// ArchiveWriteBundle is one turn ready for Firestore, including optional
// catalog revision and part documents when a single doc would exceed the limit.
type ArchiveWriteBundle struct {
	TurnIndex           int              `json:"turn_index"`
	Turn                map[string]any   `json:"turn"`
	TurnParts           []map[string]any `json:"turn_parts,omitempty"`
	CatalogRevisionID   string           `json:"catalog_revision_id,omitempty"`
	Catalog             map[string]any   `json:"catalog,omitempty"`
	CatalogParts        []map[string]any `json:"catalog_parts,omitempty"`
	CatalogChanged      bool             `json:"catalog_changed,omitempty"`
}

// BundleFromMap rebuilds an ArchiveWriteBundle from a retry-queue payload.
func BundleFromMap(row map[string]any) (*ArchiveWriteBundle, error) {
	if len(row) == 0 {
		return nil, fmt.Errorf("empty archive write bundle")
	}
	b, err := json.Marshal(row)
	if err != nil {
		return nil, err
	}
	var out ArchiveWriteBundle
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BundleToMap stores an ArchiveWriteBundle on the retry queue.
func BundleToMap(bundle *ArchiveWriteBundle) (map[string]any, error) {
	if bundle == nil {
		return nil, fmt.Errorf("nil archive write bundle")
	}
	b, err := json.Marshal(bundle)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PrepareArchiveWrite builds a Firestore write bundle from a committed turn.
// Catalog blobs are stored once per change; oversized docs are split into parts.
func PrepareArchiveWrite(committed *CommittedArchiveTurn) (*ArchiveWriteBundle, error) {
	if committed == nil {
		return nil, fmt.Errorf("nil committed archive turn")
	}
	turnMap, err := TurnToMap(committed.Turn)
	if err != nil {
		return nil, err
	}
	turnDoc, turnParts, err := splitArchiveDocument(turnMap, firestoreTurnMaxBytes)
	if err != nil {
		return nil, err
	}
	bundle := &ArchiveWriteBundle{
		TurnIndex: committed.Index,
		Turn:      turnDoc,
		TurnParts: turnParts,
	}
	if committed.CatalogRevisionID != "" {
		bundle.CatalogRevisionID = committed.CatalogRevisionID
	}
	if committed.CatalogChanged && len(committed.Catalog) > 0 {
		bundle.CatalogChanged = true
		catDoc, catParts, err := splitArchiveDocument(committed.Catalog, firestoreTurnMaxBytes)
		if err != nil {
			return nil, err
		}
		bundle.Catalog = catDoc
		bundle.CatalogParts = catParts
	}
	return bundle, nil
}

// ReassembleArchiveDocument joins a main doc and its parts into the original map.
func ReassembleArchiveDocument(main map[string]any, parts []map[string]any) (map[string]any, error) {
	if main == nil {
		return nil, nil
	}
	if !archiveDocIsSplit(main) {
		out := copyStringMap(main)
		delete(out, "index")
		return out, nil
	}
	count := anyToInt(main[archivePartCount])
	if count < 1 {
		return nil, fmt.Errorf("split archive doc missing part_count")
	}
	byPart := map[int]string{}
	for _, part := range parts {
		idx := anyToInt(part[archivePartIndex])
		data, _ := part[archivePartData].(string)
		if data == "" {
			continue
		}
		byPart[idx] = data
	}
	var b strings.Builder
	for i := 0; i < count; i++ {
		chunk, ok := byPart[i]
		if !ok {
			return nil, fmt.Errorf("missing archive part %d", i)
		}
		b.WriteString(chunk)
	}
	out := map[string]any{}
	if err := json.Unmarshal([]byte(b.String()), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func archiveDocIsSplit(doc map[string]any) bool {
	if doc == nil {
		return false
	}
	switch v := doc[archiveSplitKey].(type) {
	case bool:
		return v
	default:
		return false
	}
}

func splitArchiveDocument(doc map[string]any, maxBytes int) (map[string]any, []map[string]any, error) {
	if doc == nil {
		return nil, nil, nil
	}
	if maxBytes <= 0 {
		maxBytes = firestoreTurnMaxBytes
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, nil, err
	}
	if len(raw) <= maxBytes {
		return copyStringMap(doc), nil, nil
	}
	chunkSize := maxBytes - 64
	if chunkSize < 1024 {
		chunkSize = 1024
	}
	parts := make([]map[string]any, 0, (len(raw)/chunkSize)+1)
	for i := 0; i < len(raw); i += chunkSize {
		end := i + chunkSize
		if end > len(raw) {
			end = len(raw)
		}
		parts = append(parts, map[string]any{
			archivePartIndex: len(parts),
			archivePartData:  string(raw[i:end]),
		})
	}
	main := map[string]any{
		archiveSplitKey:  true,
		archivePartCount: len(parts),
	}
	// Keep small navigation fields on the parent turn doc for listing.
	for _, key := range []string{"at", "step", "status", "inbound"} {
		if v, ok := doc[key]; ok {
			main[key] = v
		}
	}
	return main, parts, nil
}

func copyStringMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func extractCatalogRevision(ctx map[string]any) (catalog map[string]any, fingerprint string) {
	if len(ctx) == 0 {
		return nil, ""
	}
	catalog = map[string]any{}
	for _, key := range archiveCatalogKeys {
		if v, ok := ctx[key]; ok && v != nil {
			catalog[key] = v
		}
	}
	if len(catalog) == 0 {
		return nil, ""
	}
	raw, err := json.Marshal(catalog)
	if err != nil {
		return catalog, ""
	}
	sum := sha256.Sum256(raw)
	return catalog, hex.EncodeToString(sum[:])
}

func nextCatalogRevisionID(prev string) string {
	n := 0
	if prev != "" {
		fmt.Sscanf(prev, "rev_%d", &n)
	}
	return fmt.Sprintf("rev_%04d", n+1)
}

// JoinCatalogOntoTurn merges a catalog revision into the turn context for Flow debug.
func JoinCatalogOntoTurn(turn map[string]any, catalog map[string]any) map[string]any {
	if turn == nil {
		return nil
	}
	out := copyStringMap(turn)
	if len(catalog) == 0 {
		return out
	}
	ctx, _ := out["context"].(map[string]any)
	if ctx == nil {
		ctx = map[string]any{}
	} else {
		ctx = copyStringMap(ctx)
	}
	for k, v := range catalog {
		if k == archiveSplitKey || k == archivePartCount {
			continue
		}
		ctx[k] = v
	}
	out["context"] = ctx
	return out
}

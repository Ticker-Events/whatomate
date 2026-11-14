# ADR 0004: Catalog revisions and archive parts for session archives

Accepted.

Firestore documents are limited to about 1MiB. Copying catalog blobs (`collections`, `products`, `store`, `options`) onto every turn, or truncating those values, either blew past the limit or left Flow debug with incomplete sessions.

Each turn stores messages, tool notes, and API calls in full. Catalog blobs are written as a **catalog revision** only when that set changes; the turn keeps `catalog_revision_id`. Oversized turns and revisions are split into **archive parts** and joined on read. Postgres keeps the marker and a retry queue; Firestore remains the source of truth for Flow debug. Already-written truncated sessions are left as they are.

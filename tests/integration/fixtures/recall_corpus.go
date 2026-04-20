// Package fixtures provides synthetic test data for integration tests.
//
// Design of the recall corpus:
//
//   - 32 entries (> the required 30) so two extras buffer against edge
//     cases while the 30/30 assertion still holds.
//   - Titles are distinctive multi-word phrases that uniquely identify
//     each entry; summaries contain thematically related but distinct
//     content so BM25 can discriminate when queried by verbatim title.
//   - Summaries are 20–50 words, giving BM25 a rich keyword surface.
//   - Kinds span research, decision, principle, and observation to
//     exercise the status-filter path in appraise.
//   - Topics group entries into synthetic subsystems (ratelimit, cache,
//     tracing, logging, index, migration, api, auth). Intentionally
//     generic — this corpus is synthetic and does not describe guild
//     internals.
package fixtures

// RecallEntry is one seeded knowledge entry for the recall@1 integration test.
type RecallEntry struct {
	Title   string
	Kind    string
	Summary string
	Topic   string
}

// RecallCorpus is the 32-entry fixture used by recall_test.go.
// Query each entry with its verbatim Title; assert it lands at rank 1.
//
// BM25 + recency + title-boost achieves 100% recall@1 on exact-title queries.
// This corpus reproduces that scenario end-to-end via the compiled binary.
var RecallCorpus = []RecallEntry{
	{
		Title:   "token bucket outperforms leaky bucket on bursty traffic",
		Kind:    "research",
		Summary: "A synthetic benchmark compared token bucket and leaky bucket limiters under bursty arrivals. Token bucket absorbed short bursts with fewer rejections while keeping average throughput identical. Leaky bucket smoothed output but rejected legitimate ramp-up requests.",
		Topic:   "ratelimit",
	},
	{
		Title:   "rate limiter library locked to the standard time package",
		Kind:    "decision",
		Summary: "After comparing third-party options, the standard library rate package was chosen. Burst of twenty and refill of ten requests per second were selected to match the expected traffic profile and keep dependency count low.",
		Topic:   "ratelimit",
	},
	{
		Title:   "rate limiter thresholds must be load-tested before deploy",
		Kind:    "principle",
		Summary: "Limits that look reasonable in isolation often misfire under real traffic. Every threshold change must be load-tested against representative arrival patterns before reaching production to avoid self-inflicted availability incidents.",
		Topic:   "ratelimit",
	},
	{
		Title:   "rate limiter 429 responses frequently omit the Retry-After header",
		Kind:    "observation",
		Summary: "Clients that retry too aggressively amplify load during incidents. Without a Retry-After header, well-behaved clients cannot back off correctly. Every 429 response must include the header with a sensible value in seconds.",
		Topic:   "ratelimit",
	},
	{
		Title:   "LRU cache outperforms FIFO on session read patterns",
		Kind:    "research",
		Summary: "An access-pattern study over one week of session reads showed LRU kept hit ratio eight points above FIFO. FIFO performed acceptably only when the working set fit entirely in cache. LRU generalizes better across sizes.",
		Topic:   "cache",
	},
	{
		Title:   "cache TTL locked to thirty seconds on the hot read path",
		Kind:    "decision",
		Summary: "Shorter TTLs caused excessive upstream load during coordinated invalidation. Longer TTLs exposed stale reads during deploys. Thirty seconds balanced upstream load against staleness for the hot read path.",
		Topic:   "cache",
	},
	{
		Title:   "cache stampede on cold start requires request coalescing",
		Kind:    "observation",
		Summary: "After restarts, concurrent requests for the same key stampede the upstream. Request coalescing groups in-flight lookups into a single upstream call, with followers subscribing to the result. Essential for the top endpoints.",
		Topic:   "cache",
	},
	{
		Title:   "never cache values a user can manipulate directly",
		Kind:    "principle",
		Summary: "Caching user-supplied identifiers or client-side tokens as keys creates a path for one user to read another user's cached data. Keep cache keys derived only from server-trusted inputs and user-scoped with a server-assigned prefix.",
		Topic:   "cache",
	},
	{
		Title:   "one percent trace sample captures tail latency without cost blowup",
		Kind:    "research",
		Summary: "A seven-day study showed a one-percent random sample caught every p99 tail regression in the study window while keeping ingest bills flat. Higher sampling rates added cost without catching additional incidents.",
		Topic:   "tracing",
	},
	{
		Title:   "tracing exporter locked to the OpenTelemetry OTLP protocol",
		Kind:    "decision",
		Summary: "OTLP is the cross-vendor default with first-class Go support and stable wire format. Locking to OTLP keeps the option to switch backends open without reinstrumenting every service when vendors change.",
		Topic:   "tracing",
	},
	{
		Title:   "spans without request id lose correlation across async boundaries",
		Kind:    "observation",
		Summary: "When traces cross goroutine boundaries without context propagation, the spans split into unrelated fragments. Every async dispatch must receive the context explicitly or correlation breaks for exactly the requests that are hardest to debug.",
		Topic:   "tracing",
	},
	{
		Title:   "always propagate trace context through goroutines explicitly",
		Kind:    "principle",
		Summary: "Implicit context capture is forbidden because a stale context caught in a closure leaks across requests. Each goroutine must take context as its first parameter and thread it through every downstream call.",
		Topic:   "tracing",
	},
	{
		Title:   "slog JSON handler outperforms older logging libraries on hot paths",
		Kind:    "research",
		Summary: "Micro-benchmarks against a five-field structured log at one-million calls showed slog with its JSON handler two to three times faster than older allocation-heavy loggers. Allocations per log call were also halved.",
		Topic:   "logging",
	},
	{
		Title:   "logging library locked to slog for structured output",
		Kind:    "decision",
		Summary: "Standard-library slog reached production readiness and produces JSON by default, matching the ingestion format the log pipeline expects. Locking to slog removes a dependency and keeps the logging API stable across Go versions.",
		Topic:   "logging",
	},
	{
		Title:   "log volume spikes tenfold during batch operations",
		Kind:    "observation",
		Summary: "During nightly batch jobs, per-record log lines drive a tenfold volume spike that saturates the ingest pipeline. Switching batch paths from per-record to per-chunk logging eliminates the spike without losing observability.",
		Topic:   "logging",
	},
	{
		Title:   "log at decision boundaries not inside tight loops",
		Kind:    "principle",
		Summary: "Logs inside tight loops add latency and drown signal. Log at decision boundaries — the call entering the loop and the summary of its outcome — and let counters and traces surface loop-body behavior instead.",
		Topic:   "logging",
	},
	{
		Title:   "composite index cuts session query latency by eighty percent",
		Kind:    "research",
		Summary: "A composite index on user identifier and created-at cut the session-by-user query from two hundred milliseconds to forty at the ninety-ninth percentile. The planner chose the composite over two single-column indexes every time.",
		Topic:   "index",
	},
	{
		Title:   "index strategy locked composite for sessions partial for soft deletes",
		Kind:    "decision",
		Summary: "Composite indexes cover the frequent multi-column predicates; partial indexes cover soft-deleted rows without paying the storage cost on the whole table. The combined strategy keeps storage growth manageable.",
		Topic:   "index",
	},
	{
		Title:   "index bloat after batch deletes requires VACUUM ANALYZE",
		Kind:    "observation",
		Summary: "Large batch deletes leave dead tuples in the index, inflating read latency until autovacuum catches up. Running VACUUM ANALYZE explicitly after batch operations cuts read latency in half and stabilizes the planner statistics.",
		Topic:   "index",
	},
	{
		Title:   "every production query must hit an index explicitly",
		Kind:    "principle",
		Summary: "A sequential scan in an EXPLAIN plan on a production read path is always a bug, even when the current table is small. Add the index before shipping; the table will grow before anyone notices the scan is slow.",
		Topic:   "index",
	},
	{
		Title:   "CONCURRENTLY index creation avoids DDL lock on large tables",
		Kind:    "research",
		Summary: "CREATE INDEX CONCURRENTLY avoids the exclusive lock that blocks writes for the duration of index construction. The tradeoff is doubled index build time and a two-phase commit; acceptable for every table in active production use.",
		Topic:   "migration",
	},
	{
		Title:   "migrations use numbered SQL files embedded via go embed",
		Kind:    "decision",
		Summary: "Numbered SQL files give a clear forward-only order and survive binary rebuilds via go embed. The binary ships with its own migrations, so deploy and migration are atomic — no separate migration tool required.",
		Topic:   "migration",
	},
	{
		Title:   "migration rollback rarely used in production — forward-only works",
		Kind:    "observation",
		Summary: "Across two years of deploys, rollback migrations were run fewer than five times and each was a data-shape fix, not a true reversal. Forward-only migrations with corrective follow-ups cover the real recovery path.",
		Topic:   "migration",
	},
	{
		Title:   "migrations must be idempotent and forward only",
		Kind:    "principle",
		Summary: "Every migration must be safe to re-run on a partially-applied database, because deploys crash. Forward-only keeps the history linear — corrective state-shape fixes ship as new migrations, not as reversed old ones.",
		Topic:   "migration",
	},
	{
		Title:   "gRPC outperforms REST on tight p99 latency budgets",
		Kind:    "research",
		Summary: "Head-to-head benchmarks under a ten-millisecond p99 budget showed gRPC's binary framing and HTTP/2 multiplexing saving three to five milliseconds per call. REST caught up under loose budgets but not tight ones.",
		Topic:   "api",
	},
	{
		Title:   "public API locked to REST with an OpenAPI schema",
		Kind:    "decision",
		Summary: "REST plus OpenAPI gives broadest client tooling and the lowest integration friction for external users. Internal service-to-service paths may use gRPC for latency, but the public surface stays REST to keep the audience broad.",
		Topic:   "api",
	},
	{
		Title:   "API error codes frequently confused between 400 and 422",
		Kind:    "observation",
		Summary: "Across support tickets, clients treated 400 and 422 interchangeably, leading to miscategorized failures. Standardizing on 400 for malformed requests and 422 for semantic violations with a stable code field cut the confusion.",
		Topic:   "api",
	},
	{
		Title:   "API errors must include a machine-readable code field",
		Kind:    "principle",
		Summary: "Human-readable error messages change; machine-readable codes must not. Every error response includes a stable code field that clients can branch on, plus a human message for logs and a request identifier for support tracing.",
		Topic:   "api",
	},
	{
		Title:   "JWT versus session cookies tradeoffs on cross-origin flows",
		Kind:    "research",
		Summary: "Session cookies with SameSite=Lax and Secure flags remain simpler and safer for first-party flows. JWTs win when cross-origin APIs need stateless verification, but add key rotation and revocation burden.",
		Topic:   "auth",
	},
	{
		Title:   "auth tokens locked to fifteen-minute JWT plus refresh",
		Kind:    "decision",
		Summary: "Short-lived access tokens limit the blast radius of a leak; a separately-rotated refresh token minimizes the login friction. Fifteen minutes balances revocation latency against cache hit rate on token verification.",
		Topic:   "auth",
	},
	{
		Title:   "auth bypass risk when session cookie lacks the Secure flag",
		Kind:    "observation",
		Summary: "A missing Secure flag allows the cookie to be transmitted over plain HTTP during a downgrade attack. Every production session cookie must set Secure, HttpOnly, and SameSite — verified in tests so the regression cannot sneak back in.",
		Topic:   "auth",
	},
	{
		Title:   "never pass auth tokens through URL query parameters",
		Kind:    "principle",
		Summary: "Tokens in query strings leak into server access logs, browser history, and HTTP Referer headers. Always pass tokens via the Authorization header or a secure cookie; log lines must scrub Authorization values before writing to disk.",
		Topic:   "auth",
	},
}

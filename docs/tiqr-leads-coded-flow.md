# TiQR Leads coded flow

WhatsApp coded flow that qualifies a **contact** (store + phone) against store-defined criteria. Key: `tiqr_leads`.

Unlike ecommerce, classification is not per chat session. One lead row exists per store phone.

## Registration

Package `internal/handlers/tiqrleads` registers the flow in `init()`. `chatbot_processor.go` blank-imports the package so the binary always links it.

Per-account enablement, keywords, **Set as default**, evaluation mode, and after-qualification action live on the TiQR Flows card (`CodedFlowsView`) and are stored on `CodedFlowBinding.Settings`. Criteria and the leads list live in tiqr.store.

## Contact scope

| Existing lead | Behavior |
| --- | --- |
| None | Run qualification; `create_lead` creates the row |
| `qualified=true` | Skip re-asking; load FAQs and enter the post-flow help path (no “details already collected” line) |
| `qualified=false` (partial / unclassified) | Seed saved answers, skip welcome, continue with a short “where we left off” line, ask only unanswered steps; `create_lead` upserts the same phone row when complete |

Uniqueness on the backend is `(store, phone_number)`. `session_id` is kept as the last Whatomate session that wrote the lead (audit only).

## Opening message

When the flow starts (`CurrentStep` empty), unanswered inbound texts for the session are stashed as `leads_opening_text` before the runner clears the trigger input. That text is classified once:

| Kind | Rule |
| --- | --- |
| `generic` | Every line is only a greeting or ack (`hi`, `hello`, `thanks`, …) |
| `question` | Anything else (including `Hi, is there a refund?`) |

## Shared runner behavior

TiQR Leads uses a leads-only ask path (not the shared ecommerce FAQ stop) for criterion and capture asks.

| Concern | Behavior |
| --- | --- |
| Default flow | On a new session, flow keywords are checked first. `matchDefaultCodedFlow` starts an enabled default binding only when the message matches no coded-flow or visual flow keyword |
| Translation | Outbound lines go through `Conv.text()`; language is stored when intent returns it. Detection is limited by the store’s `supported_languages` (same as ecommerce). Empty list = open detection; failed store load = `["en"]` |
| Intent | Choice paraphrases are classified onto the current buttons/rules. Exact button or list taps do not need intent |
| FAQ / side question | After the existing-lead check, `LoadFaqs("faqs", "list_faqs")`. Empty or failed FAQ load does not transfer. A grounded FAQ answer (or refuse line / short unsure line) is sent and the **same lead prompt is re-asked in the same turn**. Side questions are not stored as lead answers and do not change the score. Free-text capture fields only divert when the reply looks like a question or matches FAQ/refuse/staff |
| Call replay | Each lead ask only consumes a `_coded_calls` record whose `name` matches that step (`lead_q_*` / `lead_f_*`). Skipping an already-answered step drains that step’s call + `answer_*` marker so later steps cannot steal it. Empty answers do not count as answered and are replaced when the real choice/text lands. Post-qualify help (`How can I help you?`) must not see the last capture field’s inbound — the field accept path sets consumed before `finishQualified` |
| Refuse | Off-topic or unsafe asks hear `I can't help you with that.` and the criterion is re-asked (no invented answer, no transfer) |
| Staff request | Explicit ask for a person still transfers. Before transfer, a (partial) lead is saved (live) or classification is written for preview |
| Handoff | Mid-chat staff handoff calls `Conv.Transfer`. `TryEcommerceTransfer` does not run for this flow |
| Session expiry | When a `tiqr_leads` session idles past `SessionTimeoutMins`, Whatomate posts `create_lead` with `qualified=false` and `completion_action=session_expired` (category `unclassified`) if any answers beyond the auto Name were collected. The next chat seeds those answers and only asks the remaining steps |

## Evaluation modes

Copied onto the session on the first turn so a later Settings save does not change an in-progress chat.

| Mode | Setting value | Ask API | Score source |
| --- | --- | --- | --- |
| Fixed choices | `rules` (default) | `AskRouteButtons` (1–3 choices, label max 20) or `AskRouteList` (4–10, label max 24) | Selected choice score (0–100) |
| Rule conditions | `hybrid` | `AskRouteText` — merchant question as free text; intent allow-list is the criterion’s rules (id = rule id) | Matched rule score. Unknown id fails the grounding check |

## Score math

Lead score is the weighted sum of answered criteria: each contributes `score * weight / 100`. Enabled weights for the selected mode must total 100 (enforced when saving criteria in tiqr.store).

Category uses store thresholds (defaults: High ≥ 75, Medium ≥ 50, Low ≥ 25, else Unqualified), with High > Medium > Low.

Session-expired partials use category `unclassified` (not scored into High/Medium/Low/Unqualified). Staff classification in chat cannot pick Unclassified.

On mid-chat handoff or session expiry, unanswered criteria are omitted from the score (not treated as zero). `qualified` is true only when every enabled criterion for that mode was answered.

Session keys written for preview / handoff: `leads_score`, `leads_category`, `leads_qualified`, plus `leads_answers`.

## WhatsApp name

The contact profile name is already on the session as `contact_name`. The cached criteria `welcome_text` is not rewritten. When welcome is sent:

```
Hi {Name},
{cached welcome}
```

If the name is empty, the cached welcome is sent unchanged. The flow always stashes a default field answer `Name` = profile name (when non-empty) and sets `lead_f_name` so `create_lead` persists `Lead.name`. Name is **not** a merchant capture field.

## After qualification (and re-entry help)

Thank-you is sent once, then a FAQ-only help path runs (no guide model, no invented answers):

| Opening / reply | Behavior |
| --- | --- |
| Opening `question` | Match grounded FAQ (exact, then intent). Send stored answer, then ask if anything else. Refuse → `I can't help you with that.` then ask again. Store question with no FAQ → unsure line and `Transfer`. |
| Opening `generic` | Ask `How can I help you?` and wait |
| Follow-up decline (`no`, `thanks`, …) | Apply completion action (`record` → `End`; `handoff_high` → transfer only when High; `always_handoff` → transfer). Re-entry decline → `End` only (no re-save) |
| Follow-up store question | Same FAQ / refuse / transfer rules |

Unsure transfer line: `I'm not sure about {customer question}. I'm connecting you to a staff member who can help.` (topic cleaned from the pending unanswered text). The same unanswered question is also added to the staff-only handoff summary.

| Setting | Value | After help decline |
| --- | --- | --- |
| Record only | `record` (default) | `End` |
| Handoff High | `handoff_high` | `Transfer` only when category is High; otherwise `End` |
| Always handoff | `always_handoff` | `Transfer` |

Transfer body includes name, phone, score, category, qualified flag, each answer, and any pending unanswered question. No commerce handoff snapshot and no new Chat panel.

## Conversation outline

1. `Store("store", "get_store")` — required for language detection.
2. `Store("existing_lead", "get_lead", {phone})` — 404 / soft fail continues; qualified lead → FAQs → help path.
3. `LoadFaqs("faqs", "list_faqs")` — best-effort.
4. Bind evaluation / completion from the account settings once.
5. `Store("lead_criteria", "get_lead_criteria", {mode})` — snapshotted on the session; later turns replay the call so a mid-chat criteria edit does not change the question list.
6. Stash default Name answer; send welcome prefixed with WhatsApp name.
7. Ask each enabled criterion; FAQ / refuse stay; choice/rule matches advance.
8. Write classification onto the session, `Store("lead", "create_lead", …)` once (upsert by phone), thank-you once, then help path.

If the selected mode has no enabled criteria, the flow sends one customer-safe line and `End` (no transfer).

## Preview

`create_lead` stays on the mockable TiQR path (mock on by default). At `status === completed`, the preview sidebar shows a **Lead classification** card (score, category, qualified, answers) from session context. Mid-chat preview transfers write classification without posting a live lead.

## Store API operations (REST)

| Operation | Buyer path |
| --- | --- |
| `get_lead` | `GET /service/buyer/store/{id}/leads/current/?phone_number=` (404 → empty, soft) |
| `get_lead_criteria` | `GET /service/buyer/store/{id}/leads/criteria/?mode=` |
| `create_lead` | `POST /service/buyer/store/{id}/leads/` (upsert by phone; qualified rows are never overwritten) |

Implemented in `pkg/ticker/client.go` and `invokeTiqrStoreRESTOperation`. FAQ reuse is the existing `list_faqs` operation.

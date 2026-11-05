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
| `qualified=true` | Skip re-asking; say “We've already recorded your details.” and `End` |
| `qualified=false` (mid-chat partial) | Re-run questions; `create_lead` upserts the same phone row |

Uniqueness on the backend is `(store, phone_number)`. `session_id` is kept as the last Whatomate session that wrote the lead (audit only).

## Shared runner behavior

TiQR Leads does not ship its own translator, intent model, guide loop, FAQ answerer, or handoff queue. Every criterion ask uses the shared coded-flow route APIs with `RouteOptions{AllowFAQ: true}`.

| Concern | Behavior |
| --- | --- |
| Default flow | On a new session, flow keywords are checked first. `matchDefaultCodedFlow` starts an enabled default binding only when the message matches no coded-flow or visual flow keyword |
| Translation | Outbound lines go through `Conv.text()`; language is stored when intent returns it. Detection is limited by the store’s `supported_languages` (same as ecommerce). Empty list = open detection; failed store load = `["en"]` |
| Intent | Free text goes through `resolveFreeText` / `IdentifyCodedIntent`. Exact button or list taps do not |
| FAQ | After the existing-lead check, `LoadFaqs("faqs", "list_faqs")`. Empty or failed FAQ load does not transfer. A grounded FAQ answer is sent and the flow **stays on the same criterion**; FAQ replies are not lead answers and do not change the score |
| Guide | Below the 0.75 threshold, or route `unclear`, uses `askGuide` up to `MaxGuideTurns` (3), then handoff |
| Handoff | Route `handoff`, ungrounded intent, intent error, or used-up guide calls `Conv.Transfer`. Before transfer, a (partial) lead is saved (live) or classification is written for preview. `TryEcommerceTransfer` does not run for this flow |

## Evaluation modes

Copied onto the session on the first turn so a later Settings save does not change an in-progress chat.

| Mode | Setting value | Ask API | Score source |
| --- | --- | --- | --- |
| Fixed choices | `rules` (default) | `AskRouteButtons` (1–3 choices, label max 20) or `AskRouteList` (4–10, label max 24) | Selected choice score (0–100) |
| Rule conditions | `hybrid` | `AskRouteText` — merchant question as free text; intent allow-list is the criterion’s rules (id = rule id) | Matched rule score. Unknown id fails the grounding check |

## Score math

Lead score is the weighted sum of answered criteria: each contributes `score * weight / 100`. Enabled weights for the selected mode must total 100 (enforced when saving criteria in tiqr.store).

Category uses store thresholds (defaults: High ≥ 75, Medium ≥ 50, Low ≥ 25, else Unqualified), with High > Medium > Low.

On mid-chat handoff, unanswered criteria are omitted from the score (not treated as zero). `qualified` is true only when every enabled criterion for that mode was answered.

Session keys written for preview / handoff: `leads_score`, `leads_category`, `leads_qualified`, plus `leads_answers`.

## After qualification

| Setting | Value | Behavior |
| --- | --- | --- |
| Record only | `record` (default) | Thank-you text, `End` |
| Handoff High | `handoff_high` | `Transfer` only when category is High; otherwise thank-you and `End` |
| Always handoff | `always_handoff` | `Transfer` |

Transfer body includes name, phone, score, category, qualified flag, and each answer. No commerce handoff snapshot and no new Chat panel.

## Conversation outline

1. `Store("store", "get_store")` — required for language detection.
2. `Store("existing_lead", "get_lead", {phone})` — 404 / soft fail continues; qualified lead ends early.
3. `LoadFaqs("faqs", "list_faqs")` — best-effort.
4. Bind evaluation / completion from the account settings once.
5. `Store("lead_criteria", "get_lead_criteria", {mode})` — snapshotted on the session; later turns replay the call so a mid-chat criteria edit does not change the question list.
6. Ask each enabled criterion; FAQ matches stay; choice/rule matches advance.
7. Write classification onto the session, `Store("lead", "create_lead", …)` once (upsert by phone), then thank-you or transfer per completion setting.

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

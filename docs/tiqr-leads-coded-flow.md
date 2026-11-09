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
| `qualified=false` (mid-chat partial) | Re-run questions; `create_lead` upserts the same phone row |

Uniqueness on the backend is `(store, phone_number)`. `session_id` is kept as the last Whatomate session that wrote the lead (audit only).

## Opening message

When the flow starts (`CurrentStep` empty), unanswered inbound texts for the session are stashed as `leads_opening_text` before the runner clears the trigger input. That text is classified once:

| Kind | Rule |
| --- | --- |
| `generic` | Every line is only a greeting or ack (`hi`, `hello`, `thanks`, …) |
| `question` | Anything else (including `Hi, is there a refund?`) |

## Shared runner behavior

TiQR Leads uses the shared coded-flow route APIs with `RouteOptions{AllowFAQ: true, AllowRefuse: true}` on criterion and capture asks.

| Concern | Behavior |
| --- | --- |
| Default flow | On a new session, flow keywords are checked first. `matchDefaultCodedFlow` starts an enabled default binding only when the message matches no coded-flow or visual flow keyword |
| Translation | Outbound lines go through `Conv.text()`; language is stored when intent returns it. Detection is limited by the store’s `supported_languages` (same as ecommerce). Empty list = open detection; failed store load = `["en"]` |
| Intent | Free text goes through `resolveFreeText` / `IdentifyCodedIntent`. Exact button or list taps do not |
| FAQ | After the existing-lead check, `LoadFaqs("faqs", "list_faqs")`. Empty or failed FAQ load does not transfer. A grounded FAQ answer is sent and the flow **stays on the same criterion**; FAQ replies are not lead answers and do not change the score |
| Refuse | Route `refuse` for internal ids/secrets, malicious or jailbreak prompts, or asks unrelated to store FAQs. Customer hears `I can't help you with that.` and the criterion is re-asked (no invented answer, no transfer) |
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

Unsure transfer line: `I'm not sure about your question. I'm transferring you to a staff member who can help.` The unanswered question is added to the handoff summary.

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

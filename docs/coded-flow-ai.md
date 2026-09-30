# Coded-flow AI

Coded flows use four AI roles. Each role can use Account AI, TypeSafe Jev, or Vercel AI Gateway.

## Roles

| Role | Purpose | Settings field |
| --- | --- | --- |
| Intent | Map free text onto a route | `ai_intent_provider` |
| Translation | Translate authored lines into the customer language | `ai_translate_provider` |
| Guide | Ask one clarifying question when intent is unclear | `ai_guide_provider` |
| Order recovery | Classify create_order failures and missing fields | `ai_recover_provider` |

Default for every role is `generic` (Account AI). Account AI is the WhatsApp account provider and model on the chatbot AI tab (OpenAI, Anthropic, or Google).

## Engines

| Value | Host | Typical model | Key field |
| --- | --- | --- | --- |
| `generic` (default) | Account LLM chat completions | Account `ai_model` | `ai_api_key` |
| `jev` | `https://api.typesafe.ai/v1/systemone` | `jev-latest` | `ai_typesafe_api_key` |
| `gateway` | AI Gateway System One or chat completions | Picklist below | `ai_gateway_api_key` |

TypeSafe Jev is a System One evaluator. It fits **intent** (structured route Choice + Noul judges). It cannot generate free text, so **translation**, **guide**, and **order recovery** set to `jev` fail at runtime until those roles use Account AI or a generative Gateway model.

AI Gateway models (fixed picklist):

| Model | Use |
| --- | --- |
| `typesafe-ai/jev` (default) | Structured intent via System One |
| `google/gemini-2.5-flash` | Free-text roles (translation, guide, recovery) via chat completions |

Keys are encrypted at rest and never returned by GET; the API exposes `ai_typesafe_api_key_set` and `ai_gateway_api_key_set` instead. One TypeSafe key and one Gateway key are shared across all roles that need them.

Language models such as Gemini may answer through structured output without native confidence. When probabilities are empty, intent takes the unclear path instead of acting on a zero confidence.

Structured intent (`jev` / `gateway` with System One) does not require “Enable AI responses”. Guide, translation, and recovery on Account AI do. Gateway chat completions for those roles use the Gateway key and model only.

## Jev request shape (intent)

One System One request carries:

- A route **Choice** for the routes this step can return
- Speculative **Choice** fields for choice id, collection id, product span, and language
- **Noul** judges where two routes can both be true: `names_collection` / `names_product` on catalog steps, `asks_for_person` when choices are shown, `states_quantity` on number steps

Code composes `codedIntentResult`. Number words such as “two” are normalized to digits in code. The existing grounding check and **0.75** confidence threshold still decide whether to act or ask a guide question.

Language labels for Jev: `en`, `hi`, `ml`, `ta`, `te`, `kn`, `ar`, `es`, `manglish`, `other`.

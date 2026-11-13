# ADR 0001: TiQR Ecommerce V2 as an ADK agent with grounded tools

## Status

Accepted

## Context

TiQR Ecommerce V1 is a Go script that replays from the top on every inbound message. AI only classifies free text into routes. That design left gaps (cart line identity, swallowed “talk to staff”, silent shopping diverts on order status, sticky language, unbounded delivery pins, unused collection AI instructions).

We need a second flow that serves the same shopping purpose while the conversation is fully controlled by a model that can call store capabilities as tools. The model must be configurable per account. Invented catalog ids must not be trusted.

## Decision

Ship `tiqr_ecommerce_v2` beside V1. Each WhatsApp turn runs one Google ADK (`google.golang.org/adk`) Gemini agent with function tools for catalog, cart, fulfillment, preview, order, status, FAQ, handoff, and a single `reply` tool for WhatsApp.

Go keeps a grounding ledger of ids returned by tools and rejects any other id. Place-order and staff transfer are gated on customer confirmation. Cart, fulfillment, and a compact transcript stay on the WhatsApp session; ADK does not get its own database tables.

The Gemini model id lives on the V2 coded-flow binding; the API key is the account’s Google AI key.

## Consequences

- Accounts opt in with separate keywords; V1 remains unchanged.
- Tool coverage and grounding replace the step script and intent/guide/recover roles for this flow.
- Cross-turn memory depends on session JSON (`tiqr_cart`, `_v2_state`), which restarts cleanly with a new keyword session.
- Preview and Meta Flows still collect contact/address; the agent only decides when to open them.
- Operators configure the model without changing code; a missing Google key fails closed to staff transfer.

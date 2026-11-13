# Glossary

## Cart line

One row in the session cart: a product option together with its capture answers. Two lines can share the same option when their capture answers differ.

## Session cart

The product lines held in the WhatsApp chat session (`tiqr_cart`), including quantities, catalog prices, and capture answers. This is not the logged-in buyer cart on TiQR Store.

## Checkout preview

The priced quote returned by `POST /service/buyer/cart/checkout-preview/` for the session cart after fulfillment is known (pickup or delivery, and a pin when delivering). It is a quote, not a placed order. Amounts arrive in paise and are converted once before the customer message is built.

## Capture answer

An extra answer stored on a session-cart line (for example a message on a cake). It is shown under that line in cart and preview messages and later sent as part of the order notes.

## Waiting step

A named pause in the shopping conversation where the customer is expected to answer before the flow continues (for example welcome, collections, quantity, or details).

## Step shell

The WhatsApp message Go sends for a waiting step: the same widget and English copy as V1 for that step (reply buttons, list, carousel, image, location request, details Flow, or a text question).

## Deviation line

The optional single text bubble sent before a step shell when the agent clarifies, answers a FAQ, or explains a jump to another step.

## Binding tap

A customer tap on a button, list row, or carousel card, or the exact title of that control. The agent must perform the V1 action for that id; a contradicting tool call is rejected.

## Session archive

The operator-facing record of a coded-flow shopping session: per-turn inbound, outbound WhatsApp messages, TiQR API calls, and AI/tool notes. Stored as PreviewSink turns (Firestore for live chats). _Avoid_: session log (UI copy only).

## Flow debug

The Chat UI that loads a contact’s session archive so operators can replay what the bot sent and which tools ran.

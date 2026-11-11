# Glossary

## Session cart

The product lines held in the WhatsApp chat session (`tiqr_cart`), including quantities, catalog prices, and capture answers. This is not the logged-in buyer cart on TiQR Store.

## Checkout preview

The priced quote returned by `POST /service/buyer/cart/checkout-preview/` for the session cart after fulfillment is known (pickup or delivery, and a pin when delivering). It is a quote, not a placed order. Amounts arrive in paise and are converted once before the customer message is built.

## Capture answer

An extra answer stored on a session-cart line (for example a message on a cake). It is shown under that line in cart and preview messages and later sent as part of the order notes.

# ADR 0002: Agent names the waiting step; Go renders the V1 shell

Accepted.

V2’s free-form `reply` left customers in a text thread even though V1 shopping uses fixed WhatsApp widgets. Intent mis-routes on free text were why V2 exists, but dropping the widgets lost the step UX.

The agent still runs every customer turn and calls store tools. It ends the turn by naming a V1 waiting step (`reply.step`). Go builds that step’s shell (buttons, list, carousel, image, location, Flow, or the V1 text question) and optional chrome translations. A tap or exact title is binding. Confirm items opens the details Flow; `create_order` waits for the Flow submission. If no shell is accepted, Go resends the current step’s shell instead of sending model prose.

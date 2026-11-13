# ADR 0003: V2 ADK tools go into the shared session archive

Accepted.

Operators debug V1 through the session archive (Flow debug / Import session log). V2 already archived outbound shells and TiQR calls, but ADK tool rounds were invisible.

Every V2 tool result is recorded as a `CodedPreviewAICall` with role `adk_tool` on the same PreviewSink used for live archive and interactive preview. Interactive preview keeps a real Gemini agent and mockable TiQR; `create_order` and `transfer_to_staff` are preview-safe while capturing. We did not add a separate V2 log store or mock the model.

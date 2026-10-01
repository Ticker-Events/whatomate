package handlers

import "github.com/shridarpatil/whatomate/internal/models"

// deliverCodedText sends a chatbot line, or records it when this run is a preview.
func (a *App) deliverCodedText(ctx *chatNodeCtx, step, text string) error {
	if ctx != nil && ctx.preview != nil {
		ctx.preview.Text(step, text)
		return nil
	}
	if err := a.sendAndSaveTextMessage(ctx.account, ctx.contact, text); err != nil {
		return err
	}
	a.logSessionMessage(ctx.session.ID, models.DirectionOutgoing, text, step)
	a.LogCodedFlowWhatsApp(newCodedChat(ctx), step, "text", text)
	return nil
}

// deliverCodedCTAURL sends a CTA URL button message, or records it in preview.
// Preview does not wait for a reply — the button opens a URL and is not a choice.
func (a *App) deliverCodedCTAURL(ctx *chatNodeCtx, step, body, buttonText, url string) error {
	if ctx != nil && ctx.preview != nil {
		ctx.preview.CTAURL(step, body, buttonText, url)
		return nil
	}
	if err := a.sendAndSaveCTAURLButton(ctx.account, ctx.contact, body, buttonText, url); err != nil {
		return err
	}
	a.logSessionMessage(ctx.session.ID, models.DirectionOutgoing, body, step)
	a.LogCodedFlowWhatsApp(newCodedChat(ctx), step, "cta_url", body)
	return nil
}

// deliverCodedLocationRequest asks for a WhatsApp location pin, or records it in preview.
func (a *App) deliverCodedLocationRequest(ctx *chatNodeCtx, step, body string) error {
	if ctx != nil && ctx.preview != nil {
		ctx.preview.ExpectLocation(step, body)
		return nil
	}
	if err := a.sendAndSaveLocationRequest(ctx.account, ctx.contact, body); err != nil {
		return err
	}
	a.logSessionMessage(ctx.session.ID, models.DirectionOutgoing, body, step)
	a.LogCodedFlowWhatsApp(newCodedChat(ctx), step, "location_request", body)
	return nil
}

package tiqrecommercev2

import (
	"strings"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
)

const productLoadFailedLine = "I couldn't load that item just now. Please tap Add to cart again."
const productUnavailableLine = "Sorry, this item isn't available right now. Please choose another item."

// getProductParams is the TiQR get_product query. The buyer API requires product_id.
func getProductParams(productID string) map[string]string {
	return map[string]string{"product_id": strings.TrimSpace(productID)}
}

// tryBindProductTap advances a carousel or image "Add to cart" tap to options
// or quantity. It returns false when the button is not a product on screen.
func (rt *Runtime) tryBindProductTap(buttonID string) (bool, error) {
	id := strings.TrimSpace(buttonID)
	if id == "" || codedflow.IsShowMoreID(id) || bindingRequiredStep(id) != "" {
		return false, nil
	}
	if _, fixed := defaultFixedActions()[id]; fixed {
		return false, nil
	}
	listed := rt.sessionProduct(id)
	if listed == nil {
		return false, nil
	}

	detail, ok := rt.callTiqr("product", "get_product", getProductParams(id))
	if rt.NeedsMock {
		return true, rt.previewNeedsMockErr()
	}
	if !ok || detail == nil {
		errText := "get_product failed"
		if strings.TrimSpace(rt.LastErr) != "" {
			errText = rt.LastErr
		}
		if rt.Conv != nil && rt.Conv.ChatCtx() != nil && strings.TrimSpace(rt.Conv.ChatCtx().LastTiqrErr()) != "" {
			errText = rt.Conv.ChatCtx().LastTiqrErr()
		}
		rt.noteTool("get_product", getProductParams(id), errResult(errText))
		if opts, _ := anySlice(listed["options"]); len(opts) > 0 {
			detail = listed
		} else {
			return true, rt.sendShell(stepProducts, ShellChrome{Deviation: productLoadFailedLine})
		}
	}
	detail = unwrapProductDetail(detail)
	step, chrome := rt.stagePickedProduct(detail, listed)
	name := ""
	if detail != nil {
		name = asString(detail["name"])
	}
	rt.noteTool("select_product", getProductParams(id), okResult(map[string]any{"next": step}, name))
	return true, rt.sendShell(step, chrome)
}

func (rt *Runtime) sessionProduct(id string) map[string]any {
	rows, _ := anySlice(sessionValue(rt, "products"))
	for _, row := range rows {
		m, ok := asStringMap(row)
		if !ok {
			continue
		}
		if fieldID(m) == id {
			return m
		}
	}
	return nil
}

// stagePickedProduct stores the chosen product and returns the next waiting step.
func (rt *Runtime) stagePickedProduct(detail, listed map[string]any) (string, ShellChrome) {
	if detail == nil {
		detail = listed
	}
	id := fieldID(detail)
	name := asString(detail["name"])
	if listed != nil {
		if id == "" {
			id = fieldID(listed)
		}
		if name == "" {
			name = asString(listed["name"])
		}
	}
	opts, _ := anySlice(detail["options"])
	if len(opts) == 0 && listed != nil {
		opts, _ = anySlice(listed["options"])
		if len(opts) > 0 {
			detail["options"] = opts
		}
	}

	s := rt.session()
	if s != nil {
		if s.SessionData == nil {
			s.SessionData = models.JSONB{}
		}
		s.SessionData["product_id"] = id
		s.SessionData["product_name"] = name
		s.SessionData["product_selected"] = name
		if len(opts) > 0 {
			s.SessionData["options"] = enrichOptionsForShell(opts)
		}
		if addons, ok := anySlice(detail["addons"]); ok {
			s.SessionData["product_addons"] = addons
		}
	}
	rt.State.rememberID(id)
	groundProductDetail(rt, detail)

	if len(opts) == 0 {
		return stepProducts, ShellChrome{Deviation: productUnavailableLine}
	}
	enriched := enrichOptionsForShell(opts)
	if len(enriched) == 1 {
		m, _ := asStringMap(enriched[0])
		optID := fieldID(m)
		optName := asString(m["name"])
		if optName == "" {
			optName = name
		}
		if s != nil && s.SessionData != nil {
			s.SessionData["option_id"] = optID
			s.SessionData["option_name"] = optName
		}
		rt.State.rememberID(optID)
		return stepQuantity, ShellChrome{OptionName: optName}
	}
	return stepOptions, ShellChrome{}
}

func unwrapProductDetail(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	if opts, ok := anySlice(payload["options"]); ok && len(opts) > 0 {
		return payload
	}
	if opts, ok := anySlice(payload["active_options"]); ok && len(opts) > 0 {
		payload["options"] = opts
		return payload
	}
	for _, key := range []string{"product", "data", "result"} {
		nested, ok := asStringMap(payload[key])
		if !ok || nested == nil {
			continue
		}
		if found := unwrapProductDetail(nested); found != nil {
			if opts, ok := anySlice(found["options"]); ok && len(opts) > 0 {
				return found
			}
		}
	}
	return payload
}

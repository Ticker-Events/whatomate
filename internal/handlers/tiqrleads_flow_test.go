package handlers

import (
	"testing"

	"github.com/shridarpatil/whatomate/internal/handlers/tiqrleads"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchDefaultCodedFlow_TiqrLeads(t *testing.T) {
	app, org, account, _, _ := newGraphTestFixtures(t)

	require.NoError(t, app.DB.Create(&models.CodedFlowBinding{
		OrganizationID:  org.ID,
		WhatsAppAccount: account.Name,
		FlowKey:         tiqrleads.FlowKey,
		Keywords:        models.StringArray{"lead"},
		IsEnabled:       true,
		IsDefault:       true,
		Settings: models.JSONB{
			codedSettingEvaluationMode:   "rules",
			codedSettingCompletionAction: "record",
		},
	}).Error)

	flow := app.matchDefaultCodedFlow(org.ID, account.Name)
	require.NotNil(t, flow)
	assert.Equal(t, tiqrleads.FlowKey, flow.Key)

	eval, completion := app.LeadsFlowOptions(org.ID, account.Name)
	assert.Equal(t, "rules", eval)
	assert.Equal(t, "record", completion)
}

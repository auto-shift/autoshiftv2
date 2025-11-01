package forms

import (
	"asui/internal/data_io"
	"asui/internal/structs"

	"fyne.io/fyne/v2"
)

var (
	policies   = structs.CreatePolicies()
	allLabels  = data_io.ReadPolicyLabels()
	hubConfigs = structs.CreateHubValues()
	cSets      = hubConfigs.HubClusterSets.ClusterSets
	testVars   = structs.CreateTestVals()

	mainWin fyne.Window
)

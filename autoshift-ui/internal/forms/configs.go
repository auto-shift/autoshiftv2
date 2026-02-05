package forms

import (
	"asui/internal/data_io"
	"asui/internal/ocp"
	"fmt"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

var (
	genCard     *widget.Card
	polCard     *widget.Card
	ocpLogInBtn *widget.Button
	// preReqBtn    *widget.Button
	aSInstallBtn *widget.Button
	repoInfoBtn  *widget.Button
	deployBtn    *widget.Button
)

func Configs(win fyne.Window) fyne.CanvasObject {
	mainWin = win
	data_io.SourceTestInputs()
	configButtons()
	deployBtn = widget.NewButton("Update AutoShift", func() {})
	genCard = widget.NewCard("General:", "", container.NewVBox(ocpLogInBtn, aSInstallBtn, repoInfoBtn, layout.NewSpacer(), deployBtn))
	polCard = widget.NewCard("Cluster Sets:", "", policyButtons())
	configsGrid := container.NewGridWithRows(
		2,
		// container.NewGridWithColumns(2, gitConfigs(), deploymentConfigs()),
		container.NewGridWithColumns(2, genCard, polCard),
		container.NewStack(OutputCard()),
	)
	return configsGrid
}

func configButtons() {
	ocpLogInBtn = widget.NewButton("Login", func() {
		showOCPDialog()
	})
	aSInstallBtn = widget.NewButton("Install AutoShift", func() {
		handleInstallBtn()
	})

	repoInfoBtn = widget.NewButton("Git Repostory", func() {
		showGitDialog()
	})
	if ocp.IsLoggedIn() {
		ocpLogInBtn.SetText("Logout")
		aSInstallBtn.Enable()
		if ocp.CheckMch() == "Running" {
			aSInstallBtn.Disable()
		}
	} else {
		aSInstallBtn.Disable()
	}
}

// handlers
func handleInstallBtn() {

	aSInstallBtn.Disable()
	stat := make(chan string)
	var wg sync.WaitGroup
	var wg2 sync.WaitGroup

	wg.Add(1)
	go func() {
		wg.Done()
		stat <- ocp.CheckMch()

	}()
	wg.Wait()

	wg2.Add(2)
	go func() {
		wg2.Done()
		runCount := 1
		for {
			if ocp.CheckMch() == "Running" {
				break
			} else {
				time.Sleep(10 * time.Second)
				runCount++
				fmt.Println(<-stat)
				fmt.Println("count: ")
				fmt.Println(runCount)
			}
		}
	}()
	go func() {
		wg2.Done()
		fmt.Println(<-stat)
		if <-stat != "Running" || <-stat == "Uninstalling" {
			fmt.Println("Here somehow")
			ocp.InstallPreReqs()
		}
	}()
	wg2.Wait()

	go func() { fmt.Print("Installation Complete") }()
}

// func startInstallPr(stat chan string) {
// 	log.Println("line 91")

// 	fmt.Println("mchStat: " + <-stat)
// 	if <-stat != "Running" {
// 		fmt.Println("not running")

// 	} else {
// 		fmt.Println("already running")
// 	}
// }

func policyButtons() fyne.CanvasObject {

	policyBox := container.NewVBox()
	data_io.ReadPolicies()

	for k, v := range data_io.PData.GetHubPolicies() {
		policyBox.Add(widget.NewButton(k, func() {
			fmt.Println(&v)
			ShowPolicyModal(k, &v)
		}))
	}

	policyScroll := container.NewVScroll(policyBox)
	newPolicyBtn := widget.NewButton("Add ClusterSet", func() {})
	return container.NewBorder(
		// container.NewGridWrap(
		// 	fyne.NewSize(
		// 		400,
		// 		genCard.MinSize().Height,
		// 	),
		// 	container.NewStack(
		nil, container.NewStack(newPolicyBtn),
		nil, nil,
		container.NewStack(policyScroll),
		// 	),
		// ),
	)
}

// func genCardUpdate() {
// 	if ocp.IsLoggedIn() {

// 	}
// }

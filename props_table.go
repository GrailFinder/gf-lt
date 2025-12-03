package main

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Define constants for cell types
const (
	CellTypeCheckbox  = "checkbox"
	CellTypeDropdown  = "dropdown"
	CellTypeInput     = "input"
	CellTypeHeader    = "header"
	CellTypeListPopup = "listpopup"
)

// CellData holds additional data for each cell
type CellData struct {
	Type     string
	Options  []string
	OnChange interface{}
}

// makePropsTable creates a table-based alternative to the props form
// This allows for better key bindings and immediate effect of changes
func makePropsTable(props map[string]float32) *tview.Table {
	// Create a new table
	table := tview.NewTable().
		SetBorders(true).
		SetSelectable(true, false).
		SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorGray).Foreground(tcell.ColorWhite)) // Allow row selection but not column selection
	table.SetTitle("Properties Configuration (Press 'x' to exit)").
		SetTitleAlign(tview.AlignLeft)
	row := 0
	// Add a header or note row
	headerCell := tview.NewTableCell("Props for llamacpp completion call").
		SetTextColor(tcell.ColorYellow).
		SetAlign(tview.AlignLeft).
		SetSelectable(false)
	table.SetCell(row, 0, headerCell)
	table.SetCell(row, 1,
		tview.NewTableCell("press 'x' to exit").
			SetTextColor(tcell.ColorYellow).
			SetSelectable(false))
	row++
	// Store cell data for later use in selection functions
	cellData := make(map[string]*CellData)
	// Helper function to add a checkbox-like row
	addCheckboxRow := func(label string, initialValue bool, onChange func(bool)) {
		table.SetCell(row, 0,
			tview.NewTableCell(label).
				SetTextColor(tcell.ColorWhite).
				SetAlign(tview.AlignLeft).
				SetSelectable(false))
		valueText := "No"
		if initialValue {
			valueText = "Yes"
		}
		valueCell := tview.NewTableCell(valueText).
			SetTextColor(tcell.ColorYellow).
			SetAlign(tview.AlignCenter)
		table.SetCell(row, 1, valueCell)
		// Store cell data
		cellID := fmt.Sprintf("checkbox_%d", row)
		cellData[cellID] = &CellData{
			Type:     CellTypeCheckbox,
			OnChange: onChange,
		}
		row++
	}
	// Helper function to add a dropdown-like row, that opens a list popup
	addListPopupRow := func(label string, options []string, initialValue string, onChange func(string)) {
		table.SetCell(row, 0,
			tview.NewTableCell(label).
				SetTextColor(tcell.ColorWhite).
				SetAlign(tview.AlignLeft).
				SetSelectable(false))
		valueCell := tview.NewTableCell(initialValue).
			SetTextColor(tcell.ColorYellow).
			SetAlign(tview.AlignCenter)
		table.SetCell(row, 1, valueCell)
		// Store cell data
		cellID := fmt.Sprintf("listpopup_%d", row)
		cellData[cellID] = &CellData{
			Type:     CellTypeListPopup,
			Options:  options,
			OnChange: onChange,
		}
		row++
	}
	// Helper function to add an input field row
	addInputRow := func(label string, initialValue string, onChange func(string)) {
		table.SetCell(row, 0,
			tview.NewTableCell(label).
				SetTextColor(tcell.ColorWhite).
				SetAlign(tview.AlignLeft).
				SetSelectable(false))
		valueCell := tview.NewTableCell(initialValue).
			SetTextColor(tcell.ColorYellow).
			SetAlign(tview.AlignCenter)
		table.SetCell(row, 1, valueCell)
		// Store cell data
		cellID := fmt.Sprintf("input_%d", row)
		cellData[cellID] = &CellData{
			Type:     CellTypeInput,
			OnChange: onChange,
		}
		row++
	}
	// Add checkboxes
	addCheckboxRow("Insert <think> tag (/completion only)", cfg.ThinkUse, func(checked bool) {
		cfg.ThinkUse = checked
	})
	addCheckboxRow("RAG use", cfg.RAGEnabled, func(checked bool) {
		cfg.RAGEnabled = checked
	})
	addCheckboxRow("Inject role", injectRole, func(checked bool) {
		injectRole = checked
	})
	// Add dropdowns
	logLevels := []string{"Debug", "Info", "Warn"}
	addListPopupRow("Set log level", logLevels, GetLogLevel(), func(option string) {
		setLogLevel(option)
	})
	// Prepare API links dropdown - insert current API at the beginning
	apiLinks := slices.Insert(cfg.ApiLinks, 0, cfg.CurrentAPI)
	addListPopupRow("Select an api", apiLinks, cfg.CurrentAPI, func(option string) {
		cfg.CurrentAPI = option
	})
	// Prepare model list dropdown
	modelList := []string{chatBody.Model, "deepseek-chat", "deepseek-reasoner"}
	modelList = append(modelList, ORFreeModels...)
	addListPopupRow("Select a model", modelList, chatBody.Model, func(option string) {
		chatBody.Model = option
	})
	// Role selection dropdown
	addListPopupRow("Write next message as", listRolesWithUser(), cfg.WriteNextMsgAs, func(option string) {
		cfg.WriteNextMsgAs = option
	})
	// Add input fields
	addInputRow("New char to write msg as", "", func(text string) {
		if text != "" {
			cfg.WriteNextMsgAs = text
		}
	})
	addInputRow("Username", cfg.UserRole, func(text string) {
		if text != "" {
			renameUser(cfg.UserRole, text)
			cfg.UserRole = text
		}
	})
	// Add property fields (the float32 values)
	for propName, value := range props {
		propName := propName // capture loop variable for closure
		propValue := fmt.Sprintf("%v", value)
		addInputRow(propName, propValue, func(text string) {
			if val, err := strconv.ParseFloat(text, 32); err == nil {
				props[propName] = float32(val)
			}
		})
	}
	// Set selection function to handle dropdown-like behavior
	table.SetSelectedFunc(func(selectedRow, selectedCol int) {
		// Only handle selection on the value column (column 1)
		if selectedCol != 1 {
			// If user selects the label column, move to the value column
			if table.GetRowCount() > selectedRow && table.GetColumnCount() > 1 {
				table.Select(selectedRow, 1)
			}
			return
		}
		// Get the cell and its corresponding data
		cell := table.GetCell(selectedRow, selectedCol)
		cellID := fmt.Sprintf("checkbox_%d", selectedRow)
		// Check if it's a checkbox
		if cellData[cellID] != nil && cellData[cellID].Type == CellTypeCheckbox {
			data := cellData[cellID]
			if onChange, ok := data.OnChange.(func(bool)); ok {
				// Toggle the checkbox value
				newValue := cell.Text == "No"
				onChange(newValue)
				if newValue {
					cell.SetText("Yes")
				} else {
					cell.SetText("No")
				}
			}
			return
		}
		// Check for dropdown
		dropdownCellID := fmt.Sprintf("dropdown_%d", selectedRow)
		if cellData[dropdownCellID] != nil && cellData[dropdownCellID].Type == CellTypeDropdown {
			data := cellData[dropdownCellID]
			if onChange, ok := data.OnChange.(func(string)); ok && data.Options != nil {
				// Find current option and cycle to next
				currentValue := cell.Text
				currentIndex := -1
				for i, opt := range data.Options {
					if opt == currentValue {
						currentIndex = i
						break
					}
				}
				// Move to next option (cycle back to 0 if at end)
				nextIndex := (currentIndex + 1) % len(data.Options)
				newValue := data.Options[nextIndex]
				onChange(newValue)
				cell.SetText(newValue)
			}
			return
		}
		// Check for listpopup
		listPopupCellID := fmt.Sprintf("listpopup_%d", selectedRow)
		if cellData[listPopupCellID] != nil && cellData[listPopupCellID].Type == CellTypeListPopup {
			data := cellData[listPopupCellID]
			if onChange, ok := data.OnChange.(func(string)); ok && data.Options != nil {
				// Create a list primitive
				apiList := tview.NewList().ShowSecondaryText(false).
					SetSelectedBackgroundColor(tcell.ColorGray)
				apiList.SetTitle("Select an API").SetBorder(true)
				for i, api := range data.Options {
					if api == cell.Text {
						apiList.SetCurrentItem(i)
					}
					apiList.AddItem(api, "", 0, nil)
				}
				apiList.SetSelectedFunc(func(index int, mainText string, secondaryText string, shortcut rune) {
					onChange(mainText)
					cell.SetText(mainText)
					pages.RemovePage("apiListPopup")
				})
				apiList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
					if event.Key() == tcell.KeyEscape {
						pages.RemovePage("apiListPopup")
						return nil
					}
					return event
				})
				modal := func(p tview.Primitive, width, height int) tview.Primitive {
					return tview.NewFlex().
						AddItem(nil, 0, 1, false).
						AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
							AddItem(nil, 0, 1, false).
							AddItem(p, height, 1, true).
							AddItem(nil, 0, 1, false), width, 1, true).
						AddItem(nil, 0, 1, false)
				}
				// Add modal page and make it visible
				pages.AddPage("apiListPopup", modal(apiList, 80, 20), true, true)
				app.SetFocus(apiList)
			}
			return
		}
		// Handle input fields by creating an input modal on selection
		inputCellID := fmt.Sprintf("input_%d", selectedRow)
		if cellData[inputCellID] != nil && cellData[inputCellID].Type == CellTypeInput {
			data := cellData[inputCellID]
			if onChange, ok := data.OnChange.(func(string)); ok {
				// Create an input modal
				currentValue := cell.Text
				inputFld := tview.NewInputField()
				inputFld.SetLabel("Edit value: ")
				inputFld.SetText(currentValue)
				inputFld.SetDoneFunc(func(key tcell.Key) {
					if key == tcell.KeyEnter {
						newText := inputFld.GetText()
						onChange(newText)
						cell.SetText(newText) // Update the table cell
					}
					pages.RemovePage("editModal")
				})
				// Create a simple modal with the input field
				modalFlex := tview.NewFlex().
					SetDirection(tview.FlexRow).
					AddItem(tview.NewBox(), 0, 1, false). // Spacer
					AddItem(tview.NewFlex().
						AddItem(tview.NewBox(), 0, 1, false). // Spacer
						AddItem(inputFld, 30, 1, true).       // Input field
						AddItem(tview.NewBox(), 0, 1, false), // Spacer
										0, 1, true).
					AddItem(tview.NewBox(), 0, 1, false) // Spacer
				// Add modal page and make it visible
				pages.AddPage("editModal", modalFlex, true, true)
			}
			return
		}
	})
	// Set input capture to handle 'x' key for exiting
	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyRune && event.Rune() == 'x' {
			pages.RemovePage(propsPage)
			return nil
		}
		return event
	})
	return table
}


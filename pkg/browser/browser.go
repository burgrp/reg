package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/burgrp/reg/pkg/client"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type Browser struct {
	app          *tview.Application
	client       client.Client
	ctx          context.Context
	cancel       context.CancelFunc
	registers    map[string]*RegisterData
	registersMu  sync.RWMutex
	treeMode     bool
	showMetadata bool
	editing      bool
	editingReg   string
	filtering    bool
	filterTerm   string

	// UI components
	mainFlex      *tview.Flex
	pages         *tview.Pages
	listTable     *tview.Table
	treeView      *tview.TreeView
	metadataView  *tview.TextView
	statusBar     *tview.TextView
	editInput     *tview.InputField
	filterInput   *tview.InputField
	editOverlay   *tview.Flex
	boolRadio     *tview.Form
	boolSelection int // 0=true, 1=false, 2=null
}

type RegisterData struct {
	Name      string
	Value     any
	Metadata  map[string]any
	RemovedAt time.Time
	removeSeq uint64
}

type TreeNode struct {
	Children map[string]*TreeNode
	Register *RegisterData
}

func New(c client.Client) *Browser {
	ctx, cancel := context.WithCancel(context.Background())

	app := tview.NewApplication()
	b := &Browser{
		app:          app,
		client:       c,
		ctx:          ctx,
		cancel:       cancel,
		registers:    make(map[string]*RegisterData),
		treeMode:     false,
		showMetadata: true,
	}

	b.setupUI()
	b.setupKeyBindings()
	return b
}

func (b *Browser) setupUI() {
	// Main list/table view (flat mode)
	b.listTable = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)
	b.listTable.SetBorder(true)
	b.listTable.SetTitle(" Registers (Flat) ")
	b.listTable.SetTitleAlign(tview.AlignLeft)
	b.listTable.SetBorderColor(tcell.ColorTeal)
	b.listTable.SetTitleColor(tcell.ColorYellow)
	b.listTable.SetBackgroundColor(tcell.ColorDarkBlue)
	b.listTable.SetSelectedStyle(tcell.Style{}.
		Background(tcell.ColorTeal).
		Foreground(tcell.ColorDarkBlue))
	b.listTable.SetBorderAttributes(tcell.AttrNone)
	b.updateListTable()

	// Tree view (hierarchical mode)
	rootNode := tview.NewTreeNode("Registers").SetColor(tcell.ColorYellow)
	b.treeView = tview.NewTreeView().
		SetRoot(rootNode).
		SetCurrentNode(rootNode).
		SetTopLevel(1)
	b.treeView.SetBackgroundColor(tcell.ColorDarkBlue)
	b.treeView.SetBorder(true)
	b.treeView.SetTitle(" Registers (Tree) ")
	b.treeView.SetTitleAlign(tview.AlignLeft)
	b.treeView.SetBorderColor(tcell.ColorTeal)
	b.treeView.SetTitleColor(tcell.ColorYellow)
	b.treeView.SetBorderAttributes(tcell.AttrNone)
	b.treeView.SetGraphicsColor(tcell.ColorTeal)

	// Metadata view
	b.metadataView = tview.NewTextView().
		SetDynamicColors(true).
		SetWordWrap(true)
	b.metadataView.SetBackgroundColor(tcell.ColorDarkBlue)
	b.metadataView.SetBorder(true)
	b.metadataView.SetTitle(" Details ")
	b.metadataView.SetTitleAlign(tview.AlignLeft)
	b.metadataView.SetBorderColor(tcell.ColorTeal)
	b.metadataView.SetTitleColor(tcell.ColorYellow)
	b.metadataView.SetBorderAttributes(tcell.AttrNone)
	b.metadataView.SetText("[gray]No register selected[-]")

	// Status bar
	b.statusBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	b.statusBar.SetBackgroundColor(tcell.ColorBlack)
	b.updateStatusBar()

	// Edit input field (inline editor)
	b.editInput = tview.NewInputField().
		SetFieldWidth(0).
		SetFieldBackgroundColor(tcell.ColorTeal).
		SetFieldTextColor(tcell.ColorBlack).
		SetLabelColor(tcell.ColorBlack).
		SetDoneFunc(func(key tcell.Key) {
			if key == tcell.KeyEnter {
				b.submitEdit()
			} else if key == tcell.KeyEscape {
				b.cancelEdit()
			}
		})
	b.editInput.SetBackgroundColor(tcell.ColorSilver)
	b.editInput.SetBorder(true)
	b.editInput.SetBorderColor(tcell.ColorBlack)
	b.editInput.SetTitle(" Edit Value (JSON) ")
	b.editInput.SetBorderAttributes(tcell.AttrNone)
	b.editInput.SetTitleColor(tcell.ColorDarkBlue)

	// Filter input field
	b.filterInput = tview.NewInputField().
		SetFieldWidth(0).
		SetFieldBackgroundColor(tcell.ColorTeal).
		SetFieldTextColor(tcell.ColorBlack).
		SetLabelColor(tcell.ColorBlack).
		SetDoneFunc(func(key tcell.Key) {
			if key == tcell.KeyEnter {
				b.submitFilter()
			} else if key == tcell.KeyEscape {
				b.cancelFilter()
			}
		})
	b.filterInput.SetBackgroundColor(tcell.ColorSilver)
	b.filterInput.SetBorder(true)
	b.filterInput.SetBorderColor(tcell.ColorBlack)
	b.filterInput.SetTitle(" Filter (name contains) ")
	b.filterInput.SetBorderAttributes(tcell.AttrNone)
	b.filterInput.SetTitleColor(tcell.ColorDarkBlue)

	// Main flex layout
	b.mainFlex = tview.NewFlex().SetDirection(tview.FlexRow)

	// Content flex (registers + metadata)
	contentFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	contentFlex.AddItem(b.listTable, 0, 7, true)
	contentFlex.AddItem(b.metadataView, 0, 3, false)

	b.mainFlex.AddItem(contentFlex, 0, 1, true)
	b.mainFlex.AddItem(b.statusBar, 1, 0, false)

	// Use pages to allow overlay editing
	b.pages = tview.NewPages()
	b.pages.AddPage("main", b.mainFlex, true, true)

	b.app.SetRoot(b.pages, true)

	// Set application background to black for better contrast
	tview.Styles.PrimitiveBackgroundColor = tcell.ColorBlack
}

func (b *Browser) updateStatusBar() {
	hotKeys := map[bool][]struct {
		key    string
		action string
	}{
		true: {
			{"m", "Meta"},
			{"f", "Filter"},
			{"↵", "Edit"},
			{"t", "Flat"},
			{"e", "ExpandAll"},
			{"c", "CollapseAll"},
			{"Esc", "Quit"},
		},
		false: {
			{"m", "Meta"},
			{"f", "Filter"},
			{"↵", "Edit"},
			{"t", "Tree"},
			{"Esc", "Quit"},
		},
	}

	var parts []string
	activeHotKeys := hotKeys[b.treeMode]
	for _, hotKey := range activeHotKeys {
		parts = append(parts, fmt.Sprintf("[white:black:b]%s [black:teal:-] %s [-:-:-]", hotKey.key, hotKey.action))
	}
	b.statusBar.SetText(strings.Join(parts, "  "))
}

func (b *Browser) setupKeyBindings() {
	b.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// Let active dialogs/overlays handle Escape themselves.
		// Global Escape quits only when no dialog/overlay is active.
		if b.editing || b.filtering {
			return event
		}

		switch event.Rune() {
		case 't', 'T':
			b.toggleTreeMode()
			return nil
		case 'm', 'M':
			b.toggleMetadata()
			return nil
		case 'f', 'F':
			b.openFilter()
			return nil
		case 'e', 'E':
			if b.treeMode {
				b.expandAll()
				return nil
			}
		case 'c', 'C':
			if b.treeMode {
				b.collapseAll()
				return nil
			}
		}

		// Quit with Escape key
		if event.Key() == tcell.KeyEscape {
			b.app.Stop()
			return nil
		}

		if event.Key() == tcell.KeyEnter || event.Rune() == ' ' {
			// In tree mode, Enter can expand OR edit
			if b.treeMode {
				node := b.treeView.GetCurrentNode()
				if node != nil {
					// If it's a folder (has children), expand/collapse
					if len(node.GetChildren()) > 0 {
						node.SetExpanded(!node.IsExpanded())
						return nil
					}
					// If it's a leaf (register), edit it
					if node.GetReference() != nil {
						b.editRegister()
						return nil
					}
				}
			} else {
				b.editRegister()
				return nil
			}
		}
		return event
	})

	// Handle selection changes in list table
	b.listTable.SetSelectionChangedFunc(func(row, column int) {
		if row > 0 {
			b.updateMetadataView()
		}
	})

	// Handle selection changes in tree view
	b.treeView.SetChangedFunc(func(node *tview.TreeNode) {
		b.updateMetadataView()
	})
}

func (b *Browser) updateListTable() {
	b.registersMu.RLock()
	defer b.registersMu.RUnlock()

	// Clear table
	b.listTable.Clear()

	// Header
	b.listTable.SetCell(0, 0, tview.NewTableCell("Name").
		SetTextColor(tcell.ColorYellow).
		SetBackgroundColor(tcell.ColorDarkBlue).
		SetAttributes(tcell.AttrBold).
		SetSelectable(false))
	b.listTable.SetCell(0, 1, tview.NewTableCell("Value").
		SetTextColor(tcell.ColorYellow).
		SetBackgroundColor(tcell.ColorDarkBlue).
		SetAttributes(tcell.AttrBold).
		SetSelectable(false))

	// Get sorted register names
	names := make([]string, 0, len(b.registers))
	for name := range b.registers {
		// Apply filter if set
		if b.filterTerm != "" && !strings.Contains(name, b.filterTerm) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	// Populate rows
	for i, name := range names {
		reg := b.registers[name]
		row := i + 1
		foregroundColor := tcell.ColorWhite
		if !reg.RemovedAt.IsZero() {
			foregroundColor = tcell.ColorRed
		}

		b.listTable.SetCell(row, 0, tview.NewTableCell(name).
			SetTextColor(foregroundColor).
			SetBackgroundColor(tcell.ColorDarkBlue).
			SetReference(name))

		// Format value as JSON
		valueBytes, err := json.Marshal(reg.Value)
		var valueStr string
		if err != nil {
			valueStr = fmt.Sprintf("%v", reg.Value)
		} else {
			valueStr = string(valueBytes)
		}
		if len(valueStr) > 50 {
			valueStr = valueStr[:47] + "..."
		}
		b.listTable.SetCell(row, 1, tview.NewTableCell(valueStr).
			SetTextColor(foregroundColor).
			SetBackgroundColor(tcell.ColorDarkBlue))
	}

	// Update title with count
	title := fmt.Sprintf(" Registers (Flat) [%d] ", len(names))
	if b.filterTerm != "" {
		title = fmt.Sprintf(" Registers (Flat) [%d / %d total] ", len(names), len(b.registers))
	}
	b.listTable.SetTitle(title)
}

func (b *Browser) updateTreeView() {
	b.registersMu.RLock()
	defer b.registersMu.RUnlock()

	root := tview.NewTreeNode("Registers").
		SetColor(tcell.ColorYellow).
		SetExpanded(true)

	// Build hierarchical structure
	rootTree := &TreeNode{Children: make(map[string]*TreeNode)}

	// Count filtered registers
	filteredCount := 0
	for name, reg := range b.registers {
		// Apply filter if set
		if b.filterTerm != "" && !strings.Contains(name, b.filterTerm) {
			continue
		}
		filteredCount++

		parts := strings.Split(name, ".")
		current := rootTree

		for _, part := range parts {
			child, exists := current.Children[part]
			if !exists {
				child = &TreeNode{Children: make(map[string]*TreeNode)}
				current.Children[part] = child
			}
			current = child
		}
		current.Register = reg
	}

	b.buildTreeNodes(root, rootTree)
	b.treeView.SetRoot(root)

	// Update title with count
	title := fmt.Sprintf(" Registers (Tree) [%d] ", filteredCount)
	if b.filterTerm != "" {
		title = fmt.Sprintf(" Registers (Tree) [%d / %d total] ", filteredCount, len(b.registers))
	}
	b.treeView.SetTitle(title)
}

func (b *Browser) buildTreeNodes(parent *tview.TreeNode, tree *TreeNode) {
	if tree.Children == nil {
		return
	}

	keys := make([]string, 0, len(tree.Children))
	for k := range tree.Children {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		child := tree.Children[key]
		node := tview.NewTreeNode(key)
		node.SetSelectable(true)
		node.SetTextStyle(tcell.Style{}.Background(tcell.ColorDarkBlue))

		if child.Register != nil {
			foregroundColor := tcell.ColorWhite
			if !child.Register.RemovedAt.IsZero() {
				foregroundColor = tcell.ColorRed
			}
			// Format value as JSON
			valueBytes, err := json.Marshal(child.Register.Value)
			var valueStr string
			if err != nil {
				valueStr = fmt.Sprintf("%v", child.Register.Value)
			} else {
				valueStr = string(valueBytes)
			}
			if len(valueStr) > 30 {
				valueStr = valueStr[:27] + "..."
			}
			node.SetText(fmt.Sprintf("%s[yellow:darkblue] %s[-:-]", key, valueStr))
			node.SetColor(foregroundColor)
			node.SetReference(child.Register.Name)
		} else {
			node.SetColor(tcell.ColorSilver)
			node.SetSelectable(true)
		}

		if len(child.Children) > 0 {
			node.SetExpanded(true) // Start fully expanded
			b.buildTreeNodes(node, child)
		}

		parent.AddChild(node)
	}
}

func (b *Browser) updateMetadataView() {
	var regName string

	if b.treeMode {
		node := b.treeView.GetCurrentNode()
		if node != nil {
			if ref := node.GetReference(); ref != nil {
				regName = ref.(string)
			}
		}
	} else {
		row, _ := b.listTable.GetSelection()
		if row > 0 {
			cell := b.listTable.GetCell(row, 0)
			if ref := cell.GetReference(); ref != nil {
				regName = ref.(string)
			}
		}
	}

	if regName == "" {
		b.metadataView.SetTitle(" Details ")
		b.metadataView.SetText("[gray]No register selected[-]")
		return
	}

	b.registersMu.RLock()
	reg, exists := b.registers[regName]
	b.registersMu.RUnlock()

	if !exists {
		b.metadataView.SetTitle(" Details ")
		b.metadataView.SetText("[gray]Register not found[-]")
		return
	}

	// Set register name as title
	b.metadataView.SetTitle(fmt.Sprintf(" %s ", regName))

	// Format value as JSON
	valueBytes, err := json.Marshal(reg.Value)
	var valueStr string
	if err != nil {
		valueStr = fmt.Sprintf("%v", reg.Value)
	} else {
		valueStr = string(valueBytes)
	}

	var text strings.Builder
	text.WriteString(fmt.Sprintf("[yellow]Value:[-] %s\n\n", valueStr))

	if len(reg.Metadata) > 0 {
		text.WriteString("[yellow]Metadata:[-]\n")
		keys := make([]string, 0, len(reg.Metadata))
		for k := range reg.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			text.WriteString(fmt.Sprintf("  [cyan]%s:[-] %v\n", k, reg.Metadata[k]))
		}
	} else {
		text.WriteString("[gray]No metadata[-]")
	}

	b.metadataView.SetText(text.String())
}

func (b *Browser) toggleTreeMode() {
	b.treeMode = !b.treeMode

	// Recreate content flex with new register view
	contentFlex := tview.NewFlex().SetDirection(tview.FlexColumn)

	if b.treeMode {
		b.updateTreeView()
		b.treeView.SetCurrentNode(b.treeView.GetRoot())
		contentFlex.AddItem(b.treeView, 0, 7, true)
		b.app.SetFocus(b.treeView)
	} else {
		b.updateListTable()
		contentFlex.AddItem(b.listTable, 0, 7, true)
		b.app.SetFocus(b.listTable)
	}

	if b.showMetadata {
		contentFlex.AddItem(b.metadataView, 0, 3, false)
	}

	// Rebuild main flex
	b.mainFlex.Clear()
	b.mainFlex.AddItem(contentFlex, 0, 1, true)
	b.mainFlex.AddItem(b.statusBar, 1, 0, false)

	// Update status bar
	b.updateStatusBar()
}

func (b *Browser) toggleMetadata() {
	b.showMetadata = !b.showMetadata

	contentFlex := b.mainFlex.GetItem(0).(*tview.Flex)

	if b.showMetadata {
		contentFlex.AddItem(b.metadataView, 0, 3, false)
	} else {
		contentFlex.RemoveItem(b.metadataView)
	}
}

func (b *Browser) expandAll() {
	if !b.treeMode {
		return
	}
	root := b.treeView.GetRoot()
	b.expandNode(root)
}

func (b *Browser) expandNode(node *tview.TreeNode) {
	node.SetExpanded(true)
	for _, child := range node.GetChildren() {
		b.expandNode(child)
	}
}

func (b *Browser) collapseAll() {
	if !b.treeMode {
		return
	}
	root := b.treeView.GetRoot()
	b.collapseNode(root)
}

func (b *Browser) collapseNode(node *tview.TreeNode) {
	// Don't collapse the root node
	if node != b.treeView.GetRoot() {
		node.SetExpanded(false)
	}
	for _, child := range node.GetChildren() {
		b.collapseNode(child)
	}
}

func (b *Browser) editRegister() {
	if b.editing {
		return // Already editing
	}

	var regName string
	var row int

	if b.treeMode {
		node := b.treeView.GetCurrentNode()
		if node != nil {
			if ref := node.GetReference(); ref != nil {
				regName = ref.(string)
			}
		}
	} else {
		row, _ = b.listTable.GetSelection()
		if row > 0 {
			cell := b.listTable.GetCell(row, 0)
			if ref := cell.GetReference(); ref != nil {
				regName = ref.(string)
			}
		}
	}

	if regName == "" {
		return
	}

	b.registersMu.RLock()
	reg, exists := b.registers[regName]
	b.registersMu.RUnlock()

	if !exists {
		return
	}

	// Special case: if value is boolean, show boolean dialog.
	// Null values should use the JSON text editor.
	if _, ok := reg.Value.(bool); ok {
		b.showBooleanDialog(regName, reg.Value)
		return
	}

	// Start editing mode
	b.editing = true
	b.editingReg = regName

	// Set current value in input field as JSON
	var currentValue string
	valueBytes, err := json.Marshal(reg.Value)
	if err != nil {
		currentValue = fmt.Sprintf("%v", reg.Value)
	} else {
		currentValue = string(valueBytes)
	}
	b.editInput.SetText(currentValue)

	// Create overlay with the edit input field
	// Use a Grid to position it in the center
	grid := tview.NewGrid().
		SetColumns(0, 60, 0).
		SetRows(0, 3, 0).
		AddItem(b.editInput, 1, 1, 1, 1, 0, 0, true)

	// Make the grid semi-transparent by setting background
	grid.SetBackgroundColor(tcell.ColorDefault)

	b.pages.AddPage("edit", grid, true, true)
	b.app.SetFocus(b.editInput)
}

func (b *Browser) showBooleanDialog(regName string, currentValue any) {
	if b.editing {
		return // Already editing
	}

	b.editing = true
	b.editingReg = regName

	// Determine current selection
	currentSelection := 2 // default to null
	if currentValue != nil {
		if boolVal, ok := currentValue.(bool); ok {
			if boolVal {
				currentSelection = 0 // true
			} else {
				currentSelection = 1 // false
			}
		}
	}
	b.boolSelection = currentSelection

	// Create table with radio button style
	table := tview.NewTable()
	table.SetSelectable(true, false)
	table.SetBackgroundColor(tcell.ColorSilver)
	table.SetSelectedStyle(tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorTeal))
	table.SetBorder(true)
	table.SetTitle(fmt.Sprintf(" %s ", regName))
	table.SetBorderColor(tcell.ColorBlack)
	table.SetTitleColor(tcell.ColorDarkBlue)

	// Add three rows with radio buttons
	options := []string{"true", "false", "null"}
	updateTable := func() {
		for i, option := range options {
			var text string
			if i == b.boolSelection {
				text = "(*) " + option
			} else {
				text = "( ) " + option
			}
			cell := tview.NewTableCell(text).
				SetTextColor(tcell.ColorBlack).
				SetBackgroundColor(tcell.ColorSilver).
				SetAlign(tview.AlignLeft)

			// Highlight selected row
			if i == b.boolSelection {
				cell.SetBackgroundColor(tcell.ColorTeal)
			}

			table.SetCell(i, 0, cell)
		}
	}
	updateTable()

	// Set initial selection
	table.Select(currentSelection, 0)

	// Handle selection change
	table.SetSelectionChangedFunc(func(row, column int) {
		b.boolSelection = row
		updateTable()
	})

	// Handle Enter key to submit
	table.SetSelectedFunc(func(row, column int) {
		b.boolSelection = row
		b.submitBooleanEdit()
	})

	// Handle Escape key to cancel
	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			b.cancelBooleanEdit()
			return nil
		}
		return event
	})

	// Use a Grid to position it in the center
	grid := tview.NewGrid().
		SetColumns(0, 30, 0).
		SetRows(0, 5, 0).
		AddItem(table, 1, 1, 1, 1, 0, 0, true)

	grid.SetBackgroundColor(tcell.ColorDefault)

	b.pages.AddPage("bool-edit", grid, true, true)
	b.app.SetFocus(table)
}

func (b *Browser) submitBooleanEdit() {
	if !b.editing {
		return
	}

	regName := b.editingReg
	var newValue any

	switch b.boolSelection {
	case 0:
		newValue = true
	case 1:
		newValue = false
	case 2:
		newValue = nil
	}

	// Send change request
	go func() {
		_, requests, err := b.client.Consume(b.ctx, regName)
		if err != nil {
			return
		}
		requests <- newValue
	}()

	b.cancelBooleanEdit()
}

func (b *Browser) cancelBooleanEdit() {
	if !b.editing {
		return
	}

	// Remove dialog overlay
	b.pages.RemovePage("bool-edit")

	b.editing = false
	b.editingReg = ""

	// Return focus to register view
	if b.treeMode {
		b.app.SetFocus(b.treeView)
	} else {
		b.app.SetFocus(b.listTable)
	}
}

// getNodePath returns the path from root to the given node as a slice of node texts
func (b *Browser) getNodePath(node *tview.TreeNode) []string {
	if node == nil {
		return nil
	}

	var path []string
	current := node

	// Walk up to root, collecting node texts
	for current != nil && current != b.treeView.GetRoot() {
		path = append([]string{current.GetText()}, path...)

		// Find parent by searching the entire tree
		parent := b.findParent(b.treeView.GetRoot(), current)
		current = parent
	}

	return path
}

// findParent finds the parent node of the given child node
func (b *Browser) findParent(root *tview.TreeNode, child *tview.TreeNode) *tview.TreeNode {
	if root == nil {
		return nil
	}

	for _, c := range root.GetChildren() {
		if c == child {
			return root
		}
		if parent := b.findParent(c, child); parent != nil {
			return parent
		}
	}
	return nil
}

// selectNodeByPath selects a node by following the given path
func (b *Browser) selectNodeByPath(path []string) bool {
	if len(path) == 0 {
		return false
	}

	current := b.treeView.GetRoot()

	for _, text := range path {
		found := false
		for _, child := range current.GetChildren() {
			// Extract just the node name part (before any value display)
			childText := child.GetText()
			// For leaf nodes, text is "name[yellow] value[-]", we need just the name part
			nodeName := strings.Split(childText, "[")[0]
			searchName := strings.Split(text, "[")[0]

			if nodeName == searchName {
				current = child
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	b.treeView.SetCurrentNode(current)
	return true
}

func (b *Browser) selectTreeNode(regName string) {
	// Find and select the node with the given register name
	root := b.treeView.GetRoot()
	b.findAndSelectNode(root, regName)
}

func (b *Browser) findAndSelectNode(node *tview.TreeNode, regName string) bool {
	// Check if this node is the one we're looking for
	if ref := node.GetReference(); ref != nil {
		if ref.(string) == regName {
			b.treeView.SetCurrentNode(node)
			return true
		}
	}

	// Recursively search children
	for _, child := range node.GetChildren() {
		if b.findAndSelectNode(child, regName) {
			return true
		}
	}
	return false
}

func (b *Browser) submitEdit() {
	if !b.editing {
		return
	}

	newValue := b.editInput.GetText()
	regName := b.editingReg

	// Try to parse as JSON
	var parsedValue any

	if newValue == "null" || newValue == "nil" {
		parsedValue = nil
	} else {
		if err := json.Unmarshal([]byte(newValue), &parsedValue); err != nil {
			// If it fails, treat as string
			parsedValue = newValue
		}
	}

	// Send change request
	go func() {
		_, requests, err := b.client.Consume(b.ctx, regName)
		if err != nil {
			return
		}
		requests <- parsedValue
	}()

	b.cancelEdit()
}

func (b *Browser) cancelEdit() {
	if !b.editing {
		return
	}

	// Remove edit overlay
	b.pages.RemovePage("edit")

	b.editing = false
	regName := b.editingReg
	b.editingReg = ""

	// Restore original display
	if b.treeMode {
		b.updateTreeView()
		// Re-select the edited register
		b.selectTreeNode(regName)
	} else {
		// Restore the value in the table
		b.registersMu.RLock()
		if reg, exists := b.registers[regName]; exists {
			// Find the row for this register
			for row := 1; row < b.listTable.GetRowCount(); row++ {
				cell := b.listTable.GetCell(row, 0)
				if ref := cell.GetReference(); ref != nil && ref.(string) == regName {
					// Format value as JSON
					valueBytes, err := json.Marshal(reg.Value)
					var valueStr string
					if err != nil {
						valueStr = fmt.Sprintf("%v", reg.Value)
					} else {
						valueStr = string(valueBytes)
					}
					if len(valueStr) > 50 {
						valueStr = valueStr[:47] + "..."
					}
					b.listTable.GetCell(row, 1).SetText(valueStr)
					break
				}
			}
		}
		b.registersMu.RUnlock()
	}

	// Return focus to register view
	if b.treeMode {
		b.app.SetFocus(b.treeView)
	} else {
		b.app.SetFocus(b.listTable)
	}
}

func (b *Browser) openFilter() {
	if b.filtering {
		return // Already filtering
	}

	// Start filtering mode
	b.filtering = true

	// Set current filter term in input field
	b.filterInput.SetText(b.filterTerm)

	// Create overlay with the filter input field
	grid := tview.NewGrid().
		SetColumns(0, 60, 0).
		SetRows(0, 3, 0).
		AddItem(b.filterInput, 1, 1, 1, 1, 0, 0, true)

	grid.SetBackgroundColor(tcell.ColorDefault)

	b.pages.AddPage("filter", grid, true, true)
	b.app.SetFocus(b.filterInput)
}

func (b *Browser) submitFilter() {
	if !b.filtering {
		return
	}

	// Update filter term
	b.filterTerm = b.filterInput.GetText()

	// Remove filter overlay
	b.pages.RemovePage("filter")
	b.filtering = false

	// Refresh display with new filter
	if b.treeMode {
		b.updateTreeView()
	} else {
		b.updateListTable()
	}

	// Return focus to register view
	if b.treeMode {
		b.app.SetFocus(b.treeView)
	} else {
		b.app.SetFocus(b.listTable)
	}
}

func (b *Browser) cancelFilter() {
	if !b.filtering {
		return
	}

	// Remove filter overlay without changing filter term
	b.pages.RemovePage("filter")
	b.filtering = false

	// Return focus to register view
	if b.treeMode {
		b.app.SetFocus(b.treeView)
	} else {
		b.app.SetFocus(b.listTable)
	}
}

func (b *Browser) Run() error {
	// Start consuming all registers
	updates, _, err := b.client.ConsumeAll(b.ctx)
	if err != nil {
		return fmt.Errorf("failed to consume registers: %w", err)
	}

	// Start background goroutine to handle updates
	go func() {
		for update := range updates {
			removed := update.Removed
			name := update.Name
			var removeSeq uint64
			b.registersMu.Lock()
			if removed {
				if reg, exists := b.registers[name]; exists {
					reg.RemovedAt = time.Now()
					reg.removeSeq++
					removeSeq = reg.removeSeq
				}
			} else {
				b.registers[name] = &RegisterData{
					Name:     name,
					Value:    update.Value,
					Metadata: update.Metadata,
				}
			}
			b.registersMu.Unlock()

			if removed && removeSeq != 0 {
				go b.removeRegisterAfterDelay(name, removeSeq)
			}

			// Update UI on the main thread
			b.app.QueueUpdateDraw(func() {
				if b.treeMode {
					// Preserve current node position by saving its path
					var nodePath []string
					node := b.treeView.GetCurrentNode()
					if node != nil && node != b.treeView.GetRoot() {
						nodePath = b.getNodePath(node)
					}

					// Rebuild the tree
					b.updateTreeView()

					// Restore the node position by path
					if len(nodePath) > 0 {
						b.selectNodeByPath(nodePath)
					}
				} else {
					b.updateListTable()
				}
				b.updateMetadataView()
			})
		}
	}()

	// Run the application
	if err := b.app.Run(); err != nil {
		b.cancel()
		return err
	}

	b.cancel()
	return nil
}

func (b *Browser) removeRegisterAfterDelay(name string, removeSeq uint64) {
	select {
	case <-b.ctx.Done():
		return
	case <-time.After(5 * time.Second):
	}

	removed := false
	b.registersMu.Lock()
	if reg, exists := b.registers[name]; exists && !reg.RemovedAt.IsZero() && reg.removeSeq == removeSeq {
		delete(b.registers, name)
		removed = true
	}
	b.registersMu.Unlock()

	if !removed {
		return
	}

	b.app.QueueUpdateDraw(func() {
		if b.treeMode {
			var nodePath []string
			node := b.treeView.GetCurrentNode()
			if node != nil && node != b.treeView.GetRoot() {
				nodePath = b.getNodePath(node)
			}

			b.updateTreeView()

			if len(nodePath) > 0 {
				b.selectNodeByPath(nodePath)
			}
		} else {
			b.updateListTable()
		}
		b.updateMetadataView()
	})
}

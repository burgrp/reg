package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

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

	// UI components
	mainFlex     *tview.Flex
	pages        *tview.Pages
	listTable    *tview.Table
	treeView     *tview.TreeView
	metadataView *tview.TextView
	statusBar    *tview.TextView
	editInput    *tview.InputField
	editOverlay  *tview.Flex
}

type RegisterData struct {
	Name     string
	Value    any
	Metadata map[string]any
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
	b.listTable.SetBackgroundColor(tcell.ColorBlack)
	b.listTable.SetSelectedStyle(tcell.Style{}.
		Background(tcell.ColorTeal).
		Foreground(tcell.ColorBlack).
		Attributes(tcell.AttrBold))
	b.listTable.SetBorderAttributes(tcell.AttrNone)
	b.updateListTable()

	// Tree view (hierarchical mode)
	rootNode := tview.NewTreeNode("Registers").SetColor(tcell.ColorYellow)
	b.treeView = tview.NewTreeView().
		SetRoot(rootNode).
		SetCurrentNode(rootNode).
		SetTopLevel(1)
	b.treeView.SetBackgroundColor(tcell.ColorBlack)
	b.treeView.SetBorder(true)
	b.treeView.SetTitle(" Registers (Tree) ")
	b.treeView.SetTitleAlign(tview.AlignLeft)
	b.treeView.SetBorderColor(tcell.ColorTeal)
	b.treeView.SetTitleColor(tcell.ColorYellow)
	b.treeView.SetBorderAttributes(tcell.AttrNone)

	// Metadata view
	b.metadataView = tview.NewTextView().
		SetDynamicColors(true).
		SetWordWrap(true)
	b.metadataView.SetBackgroundColor(tcell.ColorBlack)
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
		SetLabel("").
		SetFieldWidth(0).
		SetFieldBackgroundColor(tcell.ColorBlack).
		SetFieldTextColor(tcell.ColorWhite).
		SetLabelColor(tcell.ColorYellow).
		SetDoneFunc(func(key tcell.Key) {
			if key == tcell.KeyEnter {
				b.submitEdit()
			} else if key == tcell.KeyEscape {
				b.cancelEdit()
			}
		})
	b.editInput.SetBackgroundColor(tcell.ColorBlack)
	b.editInput.SetBorder(true)
	b.editInput.SetBorderColor(tcell.ColorYellow)
	b.editInput.SetTitle(" Edit Value ")
	b.editInput.SetBorderAttributes(tcell.AttrNone)

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

/*
 Attributes:
  - b - bold
  - i - italic
  - u - underline
  - d - dim

  Reset codes:
  - [-] - reset foreground only
  - [-:-] - reset foreground and background
  - [-::-] - reset everything (color + attributes)

  Color names:
  Color names match tcell color names: black, white, red, green, blue, yellow, cyan, magenta, gray, teal, etc.
*/

func (b *Browser) updateStatusBar() {
	hotKeys := map[bool][]struct {
		key    string
		action string
	}{
		true: {
			{"m", "Meta"},
			{"↵", "Edit"},
			{"t", "Flat"},
			{"e", "ExpandAll"},
			{"c", "CollapseAll"},
			{"q", "Quit"},
		},
		false: {
			{"m", "Meta"},
			{"↵", "Edit"},
			{"t", "Tree"},
			{"q", "Quit"},
		},
	}

	var parts []string
	activeHotKeys := hotKeys[b.treeMode]
	for _, hotKey := range activeHotKeys {
		parts = append(parts, fmt.Sprintf("[white:black:b]%s [black:teal:-] %s [-:-:-]", hotKey.key, hotKey.action))
	}
	b.statusBar.SetText(strings.Join(parts, "  "))

	// if b.treeMode {
	// 	b.statusBar.SetText("[black::b]t[-::-]Flat  [black::b]m[-::-]Meta  [black::b]e[-::-]ExpandAll  [black::b]c[-::-]CollapseAll  [black::b]Enter[-::-]Edit                    [black::b]q[-::-]Quit")
	// } else {
	// 	b.statusBar.SetText("[white:black:b]t[black:teal:-] Tree [white:black:b]  m[-::-]Meta  [black::b]Enter[-::-]Edit [black::b]q[-::-]Quit")
	// }
}

func (b *Browser) setupKeyBindings() {
	b.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// Don't intercept events while editing
		if b.editing {
			return event
		}

		switch event.Rune() {
		case 't', 'T':
			b.toggleTreeMode()
			return nil
		case 'm', 'M':
			b.toggleMetadata()
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
		case 'q', 'Q':
			b.app.Stop()
			return nil
		}
		if event.Key() == tcell.KeyEnter {
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
		SetBackgroundColor(tcell.ColorBlack).
		SetAttributes(tcell.AttrBold).
		SetSelectable(false))
	b.listTable.SetCell(0, 1, tview.NewTableCell("Value").
		SetTextColor(tcell.ColorYellow).
		SetBackgroundColor(tcell.ColorBlack).
		SetAttributes(tcell.AttrBold).
		SetSelectable(false))

	// Get sorted register names
	names := make([]string, 0, len(b.registers))
	for name := range b.registers {
		names = append(names, name)
	}
	sort.Strings(names)

	// Populate rows
	for i, name := range names {
		reg := b.registers[name]
		row := i + 1

		b.listTable.SetCell(row, 0, tview.NewTableCell(name).
			SetTextColor(tcell.ColorWhite).
			SetBackgroundColor(tcell.ColorBlack).
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
			SetTextColor(tcell.ColorWhite).
			SetBackgroundColor(tcell.ColorBlack))
	}
}

func (b *Browser) updateTreeView() {
	b.registersMu.RLock()
	defer b.registersMu.RUnlock()

	root := tview.NewTreeNode("Registers").
		SetColor(tcell.ColorYellow).
		SetExpanded(true)

	// Build hierarchical structure
	rootTree := &TreeNode{Children: make(map[string]*TreeNode)}

	for name, reg := range b.registers {
		parts := strings.Split(name, ".")
		current := rootTree

		for i, part := range parts {
			if i == len(parts)-1 {
				// Leaf node - store the register
				current.Children[part] = &TreeNode{Register: reg}
			} else {
				// Intermediate node
				if _, exists := current.Children[part]; !exists {
					current.Children[part] = &TreeNode{Children: make(map[string]*TreeNode)}
				}
				current = current.Children[part]
			}
		}
	}

	b.buildTreeNodes(root, rootTree)
	b.treeView.SetRoot(root).SetCurrentNode(root)
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

		if child.Register != nil {
			// Leaf node (actual register)
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
			node.SetText(fmt.Sprintf("%s: [green]%s[-]", key, valueStr))
			node.SetColor(tcell.ColorWhite)
			node.SetReference(child.Register.Name)
		} else {
			// Intermediate node (folder)
			node.SetColor(tcell.ColorTeal)
			node.SetSelectable(true)
			node.SetExpanded(true) // Start fully expanded
			// Add children first
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

	if b.treeMode {
		// In tree mode, mark the node as being edited
		node := b.treeView.GetCurrentNode()
		if node != nil {
			// Change node text to show editing
			key := ""
			if ref := node.GetReference(); ref != nil {
				parts := strings.Split(ref.(string), ".")
				key = parts[len(parts)-1]
			}
			node.SetText(fmt.Sprintf("%s: [yellow][EDITING][-]", key))
		}
	} else {
		// In flat mode, change the value cell to show editing
		if row > 0 {
			b.listTable.GetCell(row, 1).SetText("[yellow][EDITING][-]")
		}
	}

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

func (b *Browser) submitEdit() {
	if !b.editing {
		return
	}

	newValue := b.editInput.GetText()
	regName := b.editingReg

	// Try to parse as JSON
	var parsedValue any
	if err := json.Unmarshal([]byte(newValue), &parsedValue); err != nil {
		// If it fails, treat as string
		parsedValue = newValue
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

func (b *Browser) Run() error {
	// Start consuming all registers
	updates, err := b.client.ConsumeAll(b.ctx)
	if err != nil {
		return fmt.Errorf("failed to consume registers: %w", err)
	}

	// Start background goroutine to handle updates
	go func() {
		for update := range updates {
			b.registersMu.Lock()
			b.registers[update.Name] = &RegisterData{
				Name:     update.Name,
				Value:    update.Value,
				Metadata: update.Metadata,
			}
			b.registersMu.Unlock()

			// Update UI on the main thread
			b.app.QueueUpdateDraw(func() {
				if b.treeMode {
					b.updateTreeView()
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

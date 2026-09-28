package browser

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/burgrp/reg/pkg/client"
	"github.com/rivo/tview"
)

func TestUpdateTreeViewPreservesRegisterThatIsNamespacePrefix(t *testing.T) {
	browser := &Browser{
		registers: map[string]*RegisterData{
			"rf.channel.far.state": {
				Name:  "rf.channel.far.state",
				Value: "online",
			},
			"rf.channel.far.state.since": {
				Name:  "rf.channel.far.state.since",
				Value: "2026-08-28T10:00:00Z",
			},
		},
		treeView: tview.NewTreeView(),
	}

	browser.updateTreeView()

	stateNode := findTreeNodeByReference(browser.treeView.GetRoot(), "rf.channel.far.state")
	if stateNode == nil {
		t.Fatal("state register is missing from tree")
	}
	if findTreeNodeByReference(stateNode, "rf.channel.far.state.since") == nil {
		t.Fatal("state.since register is missing below state register")
	}
}

func TestUpdateTreeViewPreservesExpansionState(t *testing.T) {
	browser := &Browser{
		registers: map[string]*RegisterData{
			"devices.kitchen.temperature": {
				Name:  "devices.kitchen.temperature",
				Value: 21,
			},
		},
		treeView: tview.NewTreeView(),
	}

	browser.updateTreeView()
	devicesNode := findTreeNodeByPath(browser.treeView.GetRoot(), "devices")
	if devicesNode == nil {
		t.Fatal("devices node is missing from tree")
	}
	browser.setTreeNodeExpanded(devicesNode, false)
	if !strings.HasSuffix(devicesNode.GetText(), collapsedNodeMarker) {
		t.Fatal("collapsed node is missing its marker")
	}

	browser.registers["status.online"] = &RegisterData{
		Name:  "status.online",
		Value: true,
	}
	browser.updateTreeView()

	devicesNode = findTreeNodeByPath(browser.treeView.GetRoot(), "devices")
	if devicesNode == nil {
		t.Fatal("devices node is missing after refresh")
	}
	if devicesNode.IsExpanded() {
		t.Fatal("manually collapsed node expanded after refresh")
	}
	if !strings.HasSuffix(devicesNode.GetText(), collapsedNodeMarker) {
		t.Fatal("collapsed node marker disappeared after refresh")
	}

	statusNode := findTreeNodeByPath(browser.treeView.GetRoot(), "status")
	if statusNode == nil {
		t.Fatal("new status node is missing after refresh")
	}
	if !statusNode.IsExpanded() {
		t.Fatal("new tree node should default to expanded")
	}

	browser.filterTerm = "status"
	browser.updateTreeView()
	browser.filterTerm = ""
	browser.updateTreeView()

	devicesNode = findTreeNodeByPath(browser.treeView.GetRoot(), "devices")
	if devicesNode == nil {
		t.Fatal("devices node is missing after clearing filter")
	}
	if devicesNode.IsExpanded() {
		t.Fatal("filtering discarded collapsed state")
	}
	if !strings.HasSuffix(devicesNode.GetText(), collapsedNodeMarker) {
		t.Fatal("filtering discarded collapsed node marker")
	}
}

func TestExpandCollapseAllUpdatesMarkers(t *testing.T) {
	browser := &Browser{
		registers: map[string]*RegisterData{
			"devices.kitchen.temperature": {
				Name:  "devices.kitchen.temperature",
				Value: 21,
			},
		},
		treeMode: true,
		treeView: tview.NewTreeView(),
	}

	browser.updateTreeView()
	browser.collapseAll()

	devicesNode := findTreeNodeByPath(browser.treeView.GetRoot(), "devices")
	if devicesNode == nil {
		t.Fatal("devices node is missing from tree")
	}
	if !strings.HasSuffix(devicesNode.GetText(), collapsedNodeMarker) {
		t.Fatal("collapse all did not add marker")
	}

	browser.expandAll()
	if strings.HasSuffix(devicesNode.GetText(), collapsedNodeMarker) {
		t.Fatal("expand all did not remove marker")
	}
}

func TestSubmitBooleanEditSendsRepeatedChanges(t *testing.T) {
	requests := make(chan client.RegisterChangeRequest, 2)
	browser := &Browser{
		ctx:            context.Background(),
		changeRequests: requests,
		app:            tview.NewApplication(),
		pages:          tview.NewPages(),
		listTable:      tview.NewTable(),
	}

	browser.editing = true
	browser.editingReg = "enabled"
	browser.boolSelection = 0
	browser.submitBooleanEdit()

	browser.editing = true
	browser.editingReg = "enabled"
	browser.boolSelection = 1
	browser.submitBooleanEdit()

	want := []bool{true, false}
	for _, wantValue := range want {
		select {
		case request := <-requests:
			if request.Name != "enabled" {
				t.Fatalf("expected request for enabled, got %q", request.Name)
			}
			if request.Value != wantValue {
				t.Fatalf("expected value %v, got %#v", wantValue, request.Value)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for value %v", wantValue)
		}
	}
}

func findTreeNodeByReference(node *tview.TreeNode, name string) *tview.TreeNode {
	if reference, ok := node.GetReference().(string); ok && reference == name {
		return node
	}
	for _, child := range node.GetChildren() {
		if found := findTreeNodeByReference(child, name); found != nil {
			return found
		}
	}
	return nil
}

func findTreeNodeByPath(root *tview.TreeNode, path ...string) *tview.TreeNode {
	current := root
	for _, segment := range path {
		var found *tview.TreeNode
		for _, child := range current.GetChildren() {
			if strings.Split(child.GetText(), "[")[0] == segment {
				found = child
				break
			}
		}
		if found == nil {
			return nil
		}
		current = found
	}
	return current
}

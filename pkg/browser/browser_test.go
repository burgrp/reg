package browser

import (
	"testing"

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

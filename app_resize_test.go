//go:build !terminal

package main

import (
	"math"
	"os/exec"
	"testing"
)

func TestViewResizePersists(t *testing.T) {
	isolateUsage(t)
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux no disponible")
	}
	mux := Tmux{WS: "resize-test-" + shortID()}
	if _, err := mux.Run("new-session", "-d", "-s", "test"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = mux.Run("kill-server") })
	for _, layout := range []string{layoutCols, layoutRows, layoutGrid} {
		view := &appView{ID: "view", Layout: layout, Slots: []string{"a", "b", "c"}}
		state := &appLayout{Tabs: []*appTab{{ID: "tab", Views: []*appView{view}}}}
		if err := mux.storeLayout(state); err != nil {
			t.Fatal(err)
		}
		weights := []float64{0.7, 1.3, 1}
		if layout == layoutGrid {
			weights = []float64{0.7, 1.3, 0.6, 1.4}
		}
		if _, err := wsAction(mux, &doReq{Action: "view.resize", View: "view", Weights: weights}); err != nil {
			t.Fatal(err)
		}
		_, saved := mux.loadLayout().findView("view")
		if len(saved.Weights) != len(weights) || saved.Weights[0] != 0.7 {
			t.Fatalf("Proporciones no guardadas: %+v", saved)
		}
		for _, invalid := range [][]float64{{1}, {math.NaN(), 1, 1}, {0, 1, 1}} {
			if _, err := wsAction(mux, &doReq{Action: "view.resize", View: "view", Weights: invalid}); err == nil {
				t.Fatal("Aceptó proporciones inválidas")
			}
		}
	}
}

// Pure-geometry tests for computeLayout/blockAt — the fixed 7-row screen
// (tui/SPEC.md §2/§4): 3 header rows, a spacer, the bars list, a
// spacer, the status row, the hint row. Only listHeight (and how many
// model blocks fit in it) responds to window size.

package ui

import "testing"

func TestComputeLayout_TooSmall(t *testing.T) {
	cases := []struct {
		name string
		w, h int
	}{
		{"below min width", minWidth - 1, 20},
		{"below min height", 80, minHeight - 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := computeLayout(c.w, c.h, 3)
			if !l.tooSmall {
				t.Fatalf("computeLayout(%d, %d, 3).tooSmall = false, want true", c.w, c.h)
			}
		})
	}
}

func TestComputeLayout_MinimumHeightFitsTwoModels(t *testing.T) {
	l := computeLayout(80, minHeight, 2)
	if l.tooSmall {
		t.Fatalf("tooSmall = true at minHeight, want false")
	}
	if l.listHeight != 7 {
		t.Fatalf("listHeight = %d, want 7 (2 models = 3*2+1)", l.listHeight)
	}
	if l.visible != 2 || l.scrolling {
		t.Fatalf("visible = %d, scrolling = %v, want 2, false", l.visible, l.scrolling)
	}
}

func TestComputeLayout_OverflowScrolls(t *testing.T) {
	l := computeLayout(80, minHeight, 5)
	if !l.scrolling {
		t.Fatalf("scrolling = false, want true: 5 models can't fit in a 7-row list")
	}
	if l.visible != 2 {
		t.Fatalf("visible = %d, want 2", l.visible)
	}
	if l.maxScroll != 3 {
		t.Fatalf("maxScroll = %d, want 3 (5 models - 2 visible)", l.maxScroll)
	}
}

func TestComputeLayout_MoreHeightShowsMoreModels(t *testing.T) {
	l := computeLayout(80, 20, 4) // listHeight = 20-7 = 13, maxVisible = (13-1)/3 = 4
	if l.scrolling {
		t.Fatalf("scrolling = true, want false: 4 models should fit in 13 rows")
	}
	if l.visible != 4 {
		t.Fatalf("visible = %d, want 4", l.visible)
	}
}

func TestComputeLayout_NoModels(t *testing.T) {
	l := computeLayout(80, minHeight, 0)
	if l.visible != 0 || l.scrolling {
		t.Fatalf("visible = %d, scrolling = %v, want 0, false", l.visible, l.scrolling)
	}
}

func TestBlockAt_TwoModelsNoScroll(t *testing.T) {
	l := computeLayout(80, minHeight, 2) // listTop=4, listHeight=7 -> rows y=4..10
	want := map[int]int{
		4:  -1, // top arrow-indicator row
		5:  0,  // model 0 name
		6:  0,  // model 0 bar
		7:  -1, // spacer between blocks
		8:  1,  // model 1 name
		9:  1,  // model 1 bar
		10: -1, // bottom arrow-indicator row
	}
	for y, exp := range want {
		if got := l.blockAt(y, 0, 2); got != exp {
			t.Errorf("blockAt(%d, 0, 2) = %d, want %d", y, got, exp)
		}
	}
}

func TestBlockAt_ScrollingOffsetsIndex(t *testing.T) {
	l := computeLayout(80, minHeight, 5) // visible=2, maxScroll=3
	scroll := 2
	if got := l.blockAt(5, scroll, 5); got != 2 {
		t.Errorf("blockAt(5, 2, 5) = %d, want 2 (scroll+0)", got)
	}
	if got := l.blockAt(8, scroll, 5); got != 3 {
		t.Errorf("blockAt(8, 2, 5) = %d, want 3 (scroll+1)", got)
	}
}

func TestClampScroll(t *testing.T) {
	l := computeLayout(80, minHeight, 5) // maxScroll = 3
	if got := l.clampScroll(-1); got != 0 {
		t.Errorf("clampScroll(-1) = %d, want 0", got)
	}
	if got := l.clampScroll(10); got != 3 {
		t.Errorf("clampScroll(10) = %d, want 3", got)
	}
	if got := l.clampScroll(2); got != 2 {
		t.Errorf("clampScroll(2) = %d, want 2", got)
	}
}

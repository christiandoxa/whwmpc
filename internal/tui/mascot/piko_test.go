package mascot

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestFramesHaveStableTerminalCells(t *testing.T) {
	for _, pose := range Gallery {
		for i := range Frames(pose) {
			for _, ascii := range []bool{true, false} {
				lines := Render(pose, i, ascii)
				if len(lines) != Height {
					t.Fatalf("%s/%d: height = %d", pose, i, len(lines))
				}
				for _, line := range lines {
					if ansi.StringWidth(line) != Width {
						t.Errorf("%s/%d: width = %d for %q", pose, i, ansi.StringWidth(line), line)
					}
					if ascii {
						for _, r := range line {
							if r < ' ' || r > '~' {
								t.Errorf("ASCII pose %s contains %q", pose, r)
							}
						}
					}
				}
			}
		}
	}
}

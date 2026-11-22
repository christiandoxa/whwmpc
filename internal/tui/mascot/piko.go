package mascot

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type Pose string

const (
	Greeting    Pose = "greeting"
	Idle        Pose = "idle"
	Blink       Pose = "blink"
	LookLeft    Pose = "look-left"
	LookRight   Pose = "look-right"
	Listening   Pose = "listening"
	Thinking    Pose = "thinking"
	Scanning    Pose = "scanning"
	SSD         Pose = "ssd"
	CPU         Pose = "cpu"
	Battery     Pose = "battery"
	Charging    Pose = "charging"
	Trim        Pose = "trim"
	Broom       Pose = "broom"
	Preview     Pose = "preview"
	Confirm     Pose = "confirmation"
	Cleaning    Pose = "cleaning"
	Applying    Pose = "applying"
	Success     Pose = "success"
	Performance Pose = "performance"
	Balanced    Pose = "balanced"
	Saver       Pose = "power-saver"
	Warning     Pose = "warning"
	Critical    Pose = "critical"
	Unknown     Pose = "unknown"
	Error       Pose = "error"
	Cancelled   Pose = "cancelled"
	Restore     Pose = "restore"
	Printing    Pose = "printing"
	Farewell    Pose = "farewell"
)

var Gallery = []Pose{Greeting, Idle, Blink, LookLeft, LookRight, Listening, Thinking, Scanning, SSD, CPU, Battery, Charging, Trim,
	Broom, Preview, Confirm, Cleaning, Applying, Success, Performance, Balanced, Saver, Warning, Critical, Unknown, Error, Cancelled, Restore, Printing, Farewell}

const Width, Height = 16, 6

type frame struct{ eyes, mouth, arm, floor, hint string }

var poses = map[Pose][]frame{
	Greeting: {
		{" .    . ", "        ", "--        ", "", "."},
		{" -    - ", "   _    ", "--        ", "", "."},
		{" o    o ", "   v    ", "-/        ", "", "+"},
		{" ^    ^ ", "   u    ", "\\!        ", "", "+"},
		{" ^    ^ ", "   u    ", "-/        ", "", "."},
		{" o    o ", "   u    ", "--        ", "", ""},
	},
	Idle:      {{" o    o ", "   u    ", "--        ", "", ""}},
	Blink:     {{" -    - ", "   u    ", "--        ", "", ""}, {" o    o ", "   u    ", "--        ", "", ""}},
	LookLeft:  {{"o    o  ", "   u    ", "--        ", "", ""}},
	LookRight: {{"  o    o", "   u    ", "--        ", "", ""}},
	Listening: {{"  o    o", "    ?   ", "--        ", "", "."}},
	Thinking: {
		{" o    o ", "    .   ", "--        ", "", ""},
		{"  o    .", "    o   ", "--        ", "", "."},
		{" -    - ", "    v   ", "--        ", "", "*"},
		{"  o    o", "    u   ", "--        ", "", "."},
	},
	Scanning: {
		{" o    O ", "   .    ", "--        ", "", "O-"},
		{" o    O ", "   .    ", "--        ", "", " O-"},
		{"  o   O ", "   .    ", "--        ", "", "  O-"},
		{"  o   O ", "   .    ", "--        ", "", " O-"},
	},
	SSD: {
		{"  o    o", "   .    ", "--        ", "    _[==]_", "J."},
		{" o    o ", "   .    ", "--        ", "    _[==]_", "J:"},
		{"  o    o", "   .    ", "--        ", "    _[==]_", "J."},
		{"  o    o", "   v    ", "--        ", "    _[==]_", "J "},
	},
	CPU: {
		{" o    o ", "   .    ", "--        ", "", "(+)"},
		{" o    o ", "   .    ", "--        ", "", "(x)"},
		{"  o    o", "   .    ", "--        ", "", "(+)"},
		{"  o    o", "   .    ", "--        ", "", "(x)"},
	},
	Battery:  {{" o    o ", "   _    ", "--        ", "", "[B]"}},
	Charging: {{" ^    ^ ", "   u    ", "--        ", "", "[+]"}},
	Trim: {
		{" o    o ", "   .    ", "--        ", "     [|][|]", "[ ]"},
		{"  o    o", "   .    ", "--        ", "     [|][|]", "[.]"},
		{" o    o ", "   .    ", "--        ", "     [|][|]", "[v]"},
		{"  o    o", "   .    ", "--        ", "     [|][|]", "[v]"},
	},
	Broom: {{" o    o ", "   u    ", "--        ", "           |//", "/"}},
	Preview: {
		{" o    o ", "   .    ", "--        ", "", "[-]"},
		{"  o    o", "   .    ", "--        ", "", "[=]"},
		{"  o    o", "   _    ", "--        ", "", "[=]"},
		{" o    o ", "   _    ", "--        ", "", "[=]"},
	},
	Confirm: {{" o    o ", "   _    ", "--        ", "", "[=]"}},
	Cleaning: {
		{"  o    o", "   .    ", "--        ", "        . /==", "/"},
		{"  o    o", "   v    ", "--        ", "        .  /==", "/"},
		{" o    o ", "   .    ", "--        ", "          . /==", "/"},
		{"  o    o", "   v    ", "--        ", "           . /==", "/"},
		{" o    o ", "   .    ", "--        ", "           /==", "/"},
	},
	Applying: {
		{"  o    o", "   .    ", "--        ", "", "{+}"},
		{"  o    o", "   .    ", "--        ", "", "{x}"},
		{" o    o ", "   .    ", "--        ", "", "{+}"},
		{" o    o ", "   .    ", "--        ", "", "{x}"},
	},
	Success: {
		{" o    o ", "   v    ", "--        ", "", " v"},
		{" ^    ^ ", "   U    ", "\\^        ", "      .", "+v"},
		{" ^    ^ ", "   U    ", "\\^        ", "   *    .", "v*"},
		{" ^    ^ ", "   U    ", "-^        ", "         .", "v "},
		{" ^    ^ ", "   u    ", "-^        ", "", "v "},
	},
	Performance: {
		{" ^    ^ ", "   u    ", "--        ", "       .>^", ""},
		{" ^    ^ ", "   u    ", "--        ", "        .>^", "*"},
		{" ^    ^ ", "   U    ", "-^        ", "          .>^", "*"},
		{" ^    ^ ", "   U    ", "-^        ", "            .>^", "v"},
		{" ^    ^ ", "   u    ", "-^        ", "", "v"},
	},
	Balanced: {{" ^    ^ ", "   u    ", "-^        ", "", "v"}},
	Saver:    {{" ^    ^ ", "   u    ", "-^        ", "", "<>"}},
	Warning:  {{" o    O ", "   _    ", "--        ", "", "!"}},
	Critical: {{" o    o ", "   _    ", "--        ", "", "!!"}},
	Unknown:  {{" o    ? ", "   _    ", "\\/        ", "", "?"}},
	Error: {
		{" ?    o ", "   o    ", "-/        ", "", "?"},
		{" o    ? ", "   ~    ", "\\/        ", "", "?"},
		{" o    ? ", "   ~    ", "--        ", "", "?"},
	},
	Cancelled: {
		{" o    o ", "   _    ", "--        ", "", "[=]"},
		{" -    - ", "   _    ", "--        ", "", "[-]"},
		{" o    o ", "   _    ", "--        ", "", "[-]"},
	},
	Restore: {
		{"  o    o", "   .    ", "--        ", "", "<<"},
		{" o    o ", "   .    ", "--        ", "", "< "},
		{"o    o  ", "   .    ", "--        ", "", "<<"},
		{" o    o ", "   .    ", "--        ", "", " <"},
	},
	Printing: {
		{" o    o ", "   u    ", "--        ", "     [---]", ""},
		{" ^    ^ ", "   u    ", "--        ", "     [===]", "."},
		{" ^    ^ ", "   v    ", "-^        ", "     [===]", "."},
		{" ^    ^ ", "   u    ", "-^        ", "     [___]", "v"},
	},
	Farewell: {
		{" ^    ^ ", "   u    ", "\\!        ", "", ""},
		{" ^    ^ ", "   u    ", "-/        ", "", ""},
		{" ^    ^ ", "   u    ", "\\!        ", "", ""},
		{" -    - ", "   u    ", "--        ", "", ""},
	},
}

func Frames(p Pose) int { return max(1, len(poses[p])) }

func art(p Pose, index int) frame {
	frames := poses[p]
	if len(frames) == 0 {
		frames = poses[Idle]
	}
	return frames[max(index, 0)%len(frames)]
}

var unicode = strings.NewReplacer("+", "✧", "*", "·", "o", "•", "^", "^", "O", "◉", "u", "ᴗ", "U", "ᵕ", "v", "✓")

func Render(p Pose, index int, ascii bool) []string {
	f := art(p, index)
	tl, tr, bl, br, line, side := ".", ".", "'", "'", "-", "|"
	if !ascii {
		tl, tr, bl, br, line, side = "╭", "╮", "╰", "╯", "─", "│"
		f.eyes, f.mouth, f.hint, f.floor = unicode.Replace(f.eyes), unicode.Replace(f.mouth), unicode.Replace(f.hint), unicode.Replace(f.floor)
	}
	hint := strings.TrimSpace(f.hint)
	arm := f.arm[:2]
	return []string{
		pad("  "+hint, Width),
		pad("  "+tl+strings.Repeat(line, 8)+tr, Width),
		pad("  "+side+f.eyes+side, Width),
		pad("  "+side+f.mouth+side, Width),
		pad(arm+bl+strings.Repeat(line, 8)+br+"-", Width),
		floor(f.floor),
	}
}

func Face(p Pose, index int, ascii bool) string {
	f := art(p, index)
	eyes := strings.Fields(f.eyes)
	if len(eyes) < 2 {
		eyes = []string{".", "."}
	}
	mouth := strings.TrimSpace(f.mouth)
	if mouth == "" {
		mouth = "."
	}
	if !ascii {
		return "[" + unicode.Replace(eyes[0]+mouth+eyes[1]) + "]"
	}
	return "[" + eyes[0] + mouth + eyes[1] + "]"
}

func pad(s string, width int) string {
	s = ansi.Truncate(s, width, "")
	return s + strings.Repeat(" ", width-ansi.StringWidth(s))
}

func floor(extra string) string {
	line := []rune(pad("    /_\\ /_\\", Width))
	for i, mark := range []rune(extra) {
		if i < len(line) && mark != ' ' {
			line[i] = mark
		}
	}
	return pad(string(line), Width)
}

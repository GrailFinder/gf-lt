package main

import (
	"regexp"
	"strings"
)

var (
	mathInline  = regexp.MustCompile(`\$([^\$]+)\$`)     // $...$
	mathDisplay = regexp.MustCompile(`\$\$([^\$]+)\$\$`) // $$...$$
)

// RenderLatex converts all LaTeX math blocks in a string to terminal‑friendly text.
func RenderLatex(text string) string {
	// Handle display math ($$...$$) – add newlines for separation
	text = mathDisplay.ReplaceAllStringFunc(text, func(match string) string {
		inner := mathDisplay.FindStringSubmatch(match)[1]
		return "\n" + convertLatex(inner) + "\n"
	})
	// Handle inline math ($...$)
	text = mathInline.ReplaceAllStringFunc(text, func(match string) string {
		inner := mathInline.FindStringSubmatch(match)[1]
		return convertLatex(inner)
	})
	return text
}

func convertLatex(s string) string {
	// ----- 1. Greek letters -----
	greek := map[string]string{
		`\alpha`: "α", `\beta`: "β", `\gamma`: "γ", `\delta`: "δ",
		`\epsilon`: "ε", `\zeta`: "ζ", `\eta`: "η", `\theta`: "θ",
		`\iota`: "ι", `\kappa`: "κ", `\lambda`: "λ", `\mu`: "μ",
		`\nu`: "ν", `\xi`: "ξ", `\pi`: "π", `\rho`: "ρ",
		`\sigma`: "σ", `\tau`: "τ", `\upsilon`: "υ", `\phi`: "φ",
		`\chi`: "χ", `\psi`: "ψ", `\omega`: "ω",
		`\Gamma`: "Γ", `\Delta`: "Δ", `\Theta`: "Θ", `\Lambda`: "Λ",
		`\Xi`: "Ξ", `\Pi`: "Π", `\Sigma`: "Σ", `\Upsilon`: "Υ",
		`\Phi`: "Φ", `\Psi`: "Ψ", `\Omega`: "Ω",
	}
	for cmd, uni := range greek {
		s = strings.ReplaceAll(s, cmd, uni)
	}

	// ----- 2. Arrows, relations, operators, symbols -----
	symbols := map[string]string{
		// Arrows
		`\leftarrow`: "←", `\rightarrow`: "→", `\leftrightarrow`: "↔",
		`\Leftarrow`: "⇐", `\Rightarrow`: "⇒", `\Leftrightarrow`: "⇔",
		`\uparrow`: "↑", `\downarrow`: "↓", `\updownarrow`: "↕",
		`\mapsto`: "↦", `\to`: "→", `\gets`: "←",
		// Relations
		`\le`: "≤", `\ge`: "≥", `\neq`: "≠", `\approx`: "≈",
		`\equiv`: "≡", `\pm`: "±", `\mp`: "∓", `\times`: "×",
		`\div`: "÷", `\cdot`: "·", `\circ`: "°", `\bullet`: "•",
		// Other symbols
		`\infty`: "∞", `\partial`: "∂", `\nabla`: "∇", `\exists`: "∃",
		`\forall`: "∀", `\in`: "∈", `\notin`: "∉", `\subset`: "⊂",
		`\subseteq`: "⊆", `\supset`: "⊃", `\supseteq`: "⊇", `\cup`: "∪",
		`\cap`: "∩", `\emptyset`: "∅", `\ell`: "ℓ", `\Re`: "ℜ",
		`\Im`: "ℑ", `\wp`: "℘", `\dag`: "†", `\ddag`: "‡",
		`\prime`: "′", `\degree`: "°", // some LLMs output \degree
	}
	for cmd, uni := range symbols {
		s = strings.ReplaceAll(s, cmd, uni)
	}

	// ----- 3. Remove \text{...} -----
	textRe := regexp.MustCompile(`\\text\{([^}]*)\}`)
	s = textRe.ReplaceAllString(s, "$1")

	// ----- 4. Fractions: \frac{a}{b} → a/b -----
	fracRe := regexp.MustCompile(`\\frac\{([^{}]*(?:\{[^{}]*\}[^{}]*)*)\}\{([^{}]*(?:\{[^{}]*\}[^{}]*)*)\}`)
	s = fracRe.ReplaceAllString(s, "$1/$2")

	// ----- 5. Remove formatting commands (\mathrm, \mathbf, etc.) -----
	for _, cmd := range []string{"mathrm", "mathbf", "mathit", "mathsf", "mathtt", "mathbb", "mathcal"} {
		re := regexp.MustCompile(`\\` + cmd + `\{([^}]*)\}`)
		s = re.ReplaceAllString(s, "$1")
	}

	// ----- 6. Subscripts and superscripts -----
	s = convertSubscripts(s)
	s = convertSuperscripts(s)

	// ----- 7. Clean up leftover braces (but keep backslashes) -----
	s = strings.ReplaceAll(s, "{", "")
	s = strings.ReplaceAll(s, "}", "")

	// ----- 8. (Optional) Remove any remaining backslash+word if you really want -----
	// But as discussed, this can break things. I'll leave it commented.
	// cmdRe := regexp.MustCompile(`\\([a-zA-Z]+)`)
	// s = cmdRe.ReplaceAllString(s, "$1")

	return s
}

// Subscript converter (handles both _{...} and _x)
func convertSubscripts(s string) string {
	subMap := map[rune]string{
		'0': "₀", '1': "₁", '2': "₂", '3': "₃", '4': "₄",
		'5': "₅", '6': "₆", '7': "₇", '8': "₈", '9': "₉",
		'+': "₊", '-': "₋", '=': "₌", '(': "₍", ')': "₎",
		'a': "ₐ", 'e': "ₑ", 'i': "ᵢ", 'o': "ₒ", 'u': "ᵤ",
		'v': "ᵥ", 'x': "ₓ",
	}
	// Braced: _{...}
	reBraced := regexp.MustCompile(`_\{([^}]*)\}`)
	s = reBraced.ReplaceAllStringFunc(s, func(match string) string {
		inner := reBraced.FindStringSubmatch(match)[1]
		return subscriptify(inner, subMap)
	})
	// Unbraced: _x (single character)
	reUnbraced := regexp.MustCompile(`_([a-zA-Z0-9])`)
	s = reUnbraced.ReplaceAllStringFunc(s, func(match string) string {
		ch := rune(match[1])
		if sub, ok := subMap[ch]; ok {
			return sub
		}
		return match // keep original _x
	})
	return s
}

func subscriptify(inner string, subMap map[rune]string) string {
	var out strings.Builder
	for _, ch := range inner {
		if sub, ok := subMap[ch]; ok {
			out.WriteString(sub)
		} else {
			return "_{" + inner + "}" // fallback
		}
	}
	return out.String()
}

// Superscript converter (handles both ^{...} and ^x)
func convertSuperscripts(s string) string {
	supMap := map[rune]string{
		'0': "⁰", '1': "¹", '2': "²", '3': "³", '4': "⁴",
		'5': "⁵", '6': "⁶", '7': "⁷", '8': "⁸", '9': "⁹",
		'+': "⁺", '-': "⁻", '=': "⁼", '(': "⁽", ')': "⁾",
		'n': "ⁿ", 'i': "ⁱ",
	}
	// Special single-character superscripts that replace the caret entirely
	specialSup := map[string]string{
		"°":  "°", // degree
		"'":  "′", // prime
		"\"": "″", // double prime
	}
	// Braced: ^{...}
	reBraced := regexp.MustCompile(`\^\{(.*?)\}`)
	s = reBraced.ReplaceAllStringFunc(s, func(match string) string {
		inner := reBraced.FindStringSubmatch(match)[1]
		return superscriptify(inner, supMap, specialSup)
	})
	// Unbraced: ^x (single character)
	reUnbraced := regexp.MustCompile(`\^([^\{[:space:]]?)`)
	s = reUnbraced.ReplaceAllStringFunc(s, func(match string) string {
		if len(match) < 2 {
			return match
		}
		ch := match[1:]
		if special, ok := specialSup[ch]; ok {
			return special
		}
		if len(ch) == 1 {
			if sup, ok := supMap[rune(ch[0])]; ok {
				return sup
			}
		}
		return match // keep ^x
	})
	return s
}

func superscriptify(inner string, supMap map[rune]string, specialSup map[string]string) string {
	if special, ok := specialSup[inner]; ok {
		return special
	}
	var out strings.Builder
	for _, ch := range inner {
		if sup, ok := supMap[ch]; ok {
			out.WriteString(sup)
		} else {
			return "^{" + inner + "}" // fallback
		}
	}
	return out.String()
}

// alignAllMarkdownTables finds every Markdown table in the document,
// realigns its columns, and returns the whole document with all tables aligned.
func alignMarkdownTables(md string) string {
	lines := strings.Split(md, "\n")
	// Precompile regexes for performance
	var (
		tagRe  = regexp.MustCompile(`\[[a-zA-Z0-9:;\-#]*\]`) // tview tags [red], [i], [turquoise::i], [-]
		mdRe   = regexp.MustCompile(`[*_` + "`" + `]`)
		ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	)
	// visualWidth strips all formatting and returns display width
	visualWidth := func(s string) int {
		plain := tagRe.ReplaceAllString(s, "")
		plain = mdRe.ReplaceAllString(plain, "")
		plain = ansiRe.ReplaceAllString(plain, "")
		return len([]rune(plain))
	}
	// alignSingleTable takes a slice of lines representing one table
	// and returns aligned lines for that table.
	alignSingleTable := func(tableLines []string) []string {
		// Parse rows, skip any separator line (contains "---" and "|")
		var rows [][]string
		for _, line := range tableLines {
			if strings.Contains(line, "---") && strings.Contains(line, "|") {
				continue
			}
			parts := strings.Split(line, "|")
			if len(parts) > 0 && parts[0] == "" {
				parts = parts[1:]
			}
			if len(parts) > 0 && parts[len(parts)-1] == "" {
				parts = parts[:len(parts)-1]
			}
			row := make([]string, len(parts))
			for j, p := range parts {
				row[j] = strings.TrimSpace(p)
			}
			rows = append(rows, row)
		}
		if len(rows) == 0 {
			return tableLines // return original if nothing left (shouldn't happen)
		}
		colCount := len(rows[0])
		// Ensure consistent column count (some rows may have extra empty cells)
		for i, row := range rows {
			if len(row) < colCount {
				for len(row) < colCount {
					row = append(row, "")
				}
				rows[i] = row
			} else if len(row) > colCount {
				rows[i] = row[:colCount]
			}
		}
		// Compute max visual width per column
		widths := make([]int, colCount)
		for _, row := range rows {
			for j := 0; j < colCount; j++ {
				w := visualWidth(row[j])
				if w > widths[j] {
					widths[j] = w
				}
			}
		}
		for j := 0; j < colCount; j++ {
			if widths[j] < 3 {
				widths[j] = 3
			}
		}
		// Rebuild aligned table
		var out []string
		for _, row := range rows {
			var b strings.Builder
			b.WriteString("|")
			for j := 0; j < colCount; j++ {
				cell := row[j]
				pad := widths[j] - visualWidth(cell)
				b.WriteString(" ")
				b.WriteString(cell)
				b.WriteString(strings.Repeat(" ", pad))
				b.WriteString(" |")
			}
			out = append(out, b.String())
		}
		return out
	}
	// Process the whole document line by line
	var resultLines []string
	i := 0
	for i < len(lines) {
		line := lines[i]
		if strings.Contains(line, "|") {
			// Start of a table block
			start := i
			for i < len(lines) && strings.Contains(lines[i], "|") {
				i++
			}
			end := i // end is exclusive
			tableBlock := lines[start:end]
			alignedBlock := alignSingleTable(tableBlock)
			resultLines = append(resultLines, alignedBlock...)
		} else {
			// Non-table line, copy as-is
			resultLines = append(resultLines, line)
			i++
		}
	}
	return strings.Join(resultLines, "\n")
}

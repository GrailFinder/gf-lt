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

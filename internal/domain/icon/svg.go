package icon

import (
	"encoding/xml"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
)

// SVGs travel as image data, never as markup inserted into the console DOM.
// Only static, self-contained drawing primitives and local fragment references
// are accepted. The existing icon request budget also bounds persisted SVGs.
func validateSVG(source string) error {
	invalid := errors.New("an SVG icon must be a bounded, static SVG with a positive viewBox and local references only")
	if len(source) > MaxIconRequestBytes {
		return invalid
	}
	decoder := xml.NewDecoder(strings.NewReader(source))
	depth, roots := 0, 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return invalid
		}
		switch t := token.(type) {
		case xml.StartElement:
			if t.Name.Space != "" && t.Name.Space != "http://www.w3.org/2000/svg" {
				return invalid
			}
			switch t.Name.Local {
			case "svg", "g", "path", "rect", "circle", "ellipse", "line", "polyline", "polygon", "defs", "clipPath", "mask", "linearGradient", "radialGradient", "stop", "use", "symbol", "title", "desc", "text", "tspan":
			default:
				return invalid
			}
			if depth == 0 {
				roots++
				if roots != 1 || t.Name.Local != "svg" {
					return invalid
				}
				viewBox := false
				for _, a := range t.Attr {
					if a.Name.Local == "viewBox" {
						viewBox = validViewBox(a.Value)
					}
				}
				if !viewBox {
					return invalid
				}
			}
			depth++
			for _, a := range t.Attr {
				name := strings.ToLower(a.Name.Local)
				value := strings.ToLower(strings.TrimSpace(a.Value))
				if strings.HasPrefix(name, "on") || name == "base" {
					return invalid
				}
				if name == "href" && !strings.HasPrefix(value, "#") {
					return invalid
				}
				if strings.Contains(value, "url(") {
					// CSS escaping and quoted URLs have many equivalent spellings; accept
					// only the simple local-fragment form instead of interpreting CSS here.
					rest := value
					for strings.Contains(rest, "url(") {
						_, tail, _ := strings.Cut(rest, "url(")
						ref, after, ok := strings.Cut(tail, ")")
						if !ok || !strings.HasPrefix(strings.TrimSpace(ref), "#") {
							return invalid
						}
						rest = after
					}
				}
				if strings.ContainsAny(value, "\\") || strings.Contains(value, "@") || strings.Contains(value, "expression(") {
					return invalid
				}
			}
		case xml.EndElement:
			depth--
		case xml.Directive:
			return invalid
		case xml.ProcInst:
			if t.Target != "xml" {
				return invalid
			}
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(t)) != "" {
				return invalid
			}
		}
	}
	if roots != 1 || depth != 0 {
		return invalid
	}
	return nil
}

func validViewBox(value string) bool {
	parts := strings.Fields(strings.ReplaceAll(value, ",", " "))
	if len(parts) != 4 {
		return false
	}
	for i, part := range parts {
		n, err := strconv.ParseFloat(part, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || (i >= 2 && n <= 0) {
			return false
		}
	}
	return true
}

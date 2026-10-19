package documents

import (
	"bytes"
	"io"
	"regexp"
	"strings"

	"github.com/aymerick/douceur/css"
	"github.com/aymerick/douceur/parser"
	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/net/html"
)

// The policy is constructed per read and never shared or mutated by callers.
// URLs are absent, except SVG references to a fragment in this same document.
func staticHTMLPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements(strings.Fields("html head body title main article section nav header footer aside div span p h1 h2 h3 h4 h5 h6 br hr pre code blockquote strong em b i u s small sub sup ul ol li dl dt dd figure figcaption table caption thead tbody tfoot tr th td colgroup col details summary a style svg g defs symbol use path rect circle ellipse line polyline polygon text tspan title desc lineargradient radialgradient stop clippath mask pattern")...)
	p.AllowAttrs("class", "id", "title", "lang", "dir", "role", "aria-label", "aria-hidden", "style").Globally()
	p.AllowAttrs("open").OnElements("details")
	p.AllowAttrs("colspan", "rowspan", "scope").OnElements("th", "td")
	p.AllowAttrs("start", "reversed").OnElements("ol")
	p.AllowAttrs("viewbox", "preserveaspectratio", "width", "height", "x", "y", "x1", "y1", "x2", "y2", "cx", "cy", "r", "rx", "ry", "d", "points", "transform", "opacity", "fill-opacity", "stroke-opacity", "stroke-width", "stroke-linecap", "stroke-linejoin", "stroke-dasharray", "text-anchor", "dominant-baseline", "font-size", "offset", "gradientunits", "gradienttransform", "patternunits", "patterntransform").OnElements(strings.Fields("svg g symbol use path rect circle ellipse line polyline polygon text tspan lineargradient radialgradient stop clippath mask pattern")...)
	p.AllowAttrs("fill", "stroke", "stop-color", "clip-path", "mask").Matching(regexp.MustCompile(`(?i)^(?:[a-z]+|#[0-9a-f]+|url\(#[a-z0-9_-]+\))$`)).Globally()
	p.AllowAttrs("href", "xlink:href").Matching(regexp.MustCompile(`^#[a-zA-Z0-9_-]+$`)).OnElements("use")
	// Only style is allowed by the element list. Its CSS is separately parsed
	// and filtered below; AllowUnsafe alone does not sanitize CSS text.
	p.AllowUnsafe(true)
	p.AllowElementsContent("style")
	p.SkipElementsContent("script", "iframe", "object", "embed", "form", "foreignobject", "animate", "animatetransform", "set", "noscript", "template")
	return p
}

// sanitizeHTML runs the mature HTML allowlist first, then rewrites CSS using a
// parsed declaration allowlist. The final HTML pass protects serialization too.
func sanitizeHTML(body []byte) []byte {
	p := staticHTMLPolicy()
	z := html.NewTokenizer(bytes.NewReader(p.SanitizeBytes(body)))
	var out bytes.Buffer
	inStyle := false
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if z.Err() != io.EOF {
				return nil
			}
			break
		}
		t := z.Token()
		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			inStyle = t.Data == "style"
			attrs := t.Attr[:0]
			for _, a := range t.Attr {
				if a.Key == "style" {
					a.Val = sanitizeDeclarations(a.Val)
				}
				attrs = append(attrs, a)
			}
			t.Attr = attrs
			out.WriteString(t.String())
		case html.EndTagToken:
			inStyle = false
			out.WriteString(t.String())
		case html.TextToken:
			if inStyle {
				out.WriteString(sanitizeStylesheet(t.Data))
			} else {
				out.WriteString(t.String())
			}
		}
	}
	return p.SanitizeBytes(out.Bytes())
}

var staticCSSProperties = strings.Fields("color background background-color background-image display position top right bottom left inset box-sizing width min-width max-width height min-height max-height margin margin-top margin-right margin-bottom margin-left padding padding-top padding-right padding-bottom padding-left border border-top border-right border-bottom border-left border-color border-width border-style border-radius border-collapse border-spacing box-shadow font font-family font-size font-weight font-style line-height letter-spacing text-align text-decoration text-transform text-overflow white-space word-break overflow overflow-x overflow-y vertical-align list-style list-style-type table-layout caption-side opacity visibility flex flex-direction flex-wrap flex-grow flex-shrink flex-basis align-items align-content align-self justify-content justify-items justify-self gap row-gap column-gap grid grid-template-columns grid-template-rows grid-column grid-row grid-auto-flow grid-auto-columns grid-auto-rows aspect-ratio fill stroke stroke-width stroke-dasharray text-anchor transform transform-origin")
var staticCSSValue = regexp.MustCompile(`^[a-zA-Z0-9\s#%.,()'"+*/_-]+$`)
var cssFunction = regexp.MustCompile(`([a-zA-Z_-][a-zA-Z0-9_-]*)\s*\(`)

func safeCSSValue(value string) bool {
	if !staticCSSValue.MatchString(value) || strings.Contains(value, "/*") {
		return false
	}
	for _, match := range cssFunction.FindAllStringSubmatch(value, -1) {
		switch strings.ToLower(match[1]) {
		case "var", "rgb", "rgba", "hsl", "hsla", "calc", "min", "max", "clamp", "repeat", "minmax", "linear-gradient", "radial-gradient", "translate", "translatex", "translatey", "scale", "rotate":
		default:
			return false
		}
	}
	return true
}

func filteredDeclarations(declarations []*css.Declaration) string {
	var out strings.Builder
	for _, d := range declarations {
		if strings.HasPrefix(d.Property, "--") && regexp.MustCompile(`^--[a-zA-Z0-9_-]+$`).MatchString(d.Property) && safeCSSValue(d.Value) {
			out.WriteString(d.String())
			continue
		}
		for _, property := range staticCSSProperties {
			if strings.EqualFold(d.Property, property) && safeCSSValue(d.Value) {
				out.WriteString(d.String())
				break
			}
		}
	}
	return out.String()
}

func sanitizeDeclarations(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasSuffix(value, ";") {
		value += ";"
	}
	d, err := parser.ParseDeclarations(value)
	if err != nil {
		return ""
	}
	return filteredDeclarations(d)
}

func sanitizeStylesheet(value string) string {
	sheet, err := parser.Parse(value)
	if err != nil {
		return ""
	}
	var out strings.Builder
	// Only media rules may contain other rules; resource at-rules are omitted.
	var writeRules func([]*css.Rule)
	writeRules = func(rules []*css.Rule) {
		for _, r := range rules {
			if strings.ContainsAny(r.Prelude, "\\<@") {
				continue
			}
			if r.Kind == css.QualifiedRule {
				out.WriteString(r.Prelude + "{" + filteredDeclarations(r.Declarations) + "}")
			} else if r.Name == "@media" && safeCSSMedia(r.Prelude) {
				out.WriteString("@media " + r.Prelude + "{")
				writeRules(r.Rules)
				out.WriteString("}")
			}
		}
	}
	writeRules(sheet.Rules)
	return out.String()
}

func safeCSSMedia(value string) bool {
	return regexp.MustCompile(`^[a-zA-Z0-9\s():.,/_-]+$`).MatchString(value)
}

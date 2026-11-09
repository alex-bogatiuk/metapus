package pdfrender

import (
	"bytes"
	"fmt"
	"strings"

	"golang.org/x/net/html"
	"metapus/internal/domain/printing"
)

const contentPolicy = "default-src 'none'; script-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'"

// Parse and rebuild rather than editing strings: no source meta refresh, base,
// scripts, frames, SVG/MathML foreign content, event handlers or URL attributes
// survive. CSS cannot load remote resources because CSP precedes all content.
func prepareHTML(source []byte, options printing.PDFOptions) ([]byte, error) {
	if len(source) == 0 || len(source) > MaxHTML {
		return nil, fmt.Errorf("invalid HTML size")
	}
	if options.PaperSize == "" {
		options.PaperSize = "A4"
	}
	if options.PaperSize != "A4" && options.PaperSize != "A5" && options.PaperSize != "Letter" {
		return nil, fmt.Errorf("unsupported paper size")
	}
	doc, err := html.Parse(bytes.NewReader(source))
	if err != nil {
		return nil, err
	}
	allowed := strings.Fields("html head body title style div span p br hr h1 h2 h3 h4 h5 h6 table thead tbody tfoot tr th td col colgroup b strong i em u s small sup sub ul ol li pre code blockquote img")
	tags := make(map[string]bool, len(allowed))
	for _, tag := range allowed {
		tags[tag] = true
	}
	var clean func(*html.Node)
	clean = func(parent *html.Node) {
		for node := parent.FirstChild; node != nil; {
			next := node.NextSibling
			if node.Type == html.CommentNode || (node.Type == html.ElementNode && (!tags[node.Data] || node.Namespace != "")) {
				parent.RemoveChild(node)
			} else {
				attrs := node.Attr[:0]
				for _, attr := range node.Attr {
					if attr.Namespace != "" {
						continue
					}
					switch attr.Key {
					case "class", "id", "style", "lang", "dir", "colspan", "rowspan", "width", "height", "align", "valign", "border", "cellpadding", "cellspacing", "alt":
						attrs = append(attrs, attr)
					case "src":
						if node.Data == "img" && (strings.HasPrefix(attr.Val, "data:image/png;base64,") || strings.HasPrefix(attr.Val, "data:image/jpeg;base64,")) {
							attrs = append(attrs, attr)
						}
					}
				}
				node.Attr = attrs
				clean(node)
			}
			node = next
		}
	}
	clean(doc)
	var head *html.Node
	var findHead func(*html.Node)
	findHead = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "head" {
			head = n
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			findHead(child)
		}
	}
	findHead(doc)
	if head == nil {
		return nil, fmt.Errorf("missing HTML head")
	}
	head.InsertBefore(&html.Node{Type: html.ElementNode, Data: "meta", Attr: []html.Attribute{{Key: "http-equiv", Val: "Content-Security-Policy"}, {Key: "content", Val: contentPolicy}}}, head.FirstChild)
	head.InsertBefore(&html.Node{Type: html.ElementNode, Data: "meta", Attr: []html.Attribute{{Key: "charset", Val: "utf-8"}}}, head.FirstChild)
	orientation := "portrait"
	if options.Landscape {
		orientation = "landscape"
	}
	pageStyle := &html.Node{Type: html.ElementNode, Data: "style"}
	pageStyle.AppendChild(&html.Node{Type: html.TextNode, Data: "@page { size: " + options.PaperSize + " " + orientation + "; }"})
	head.AppendChild(pageStyle)
	var out bytes.Buffer
	if err := html.Render(&out, doc); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

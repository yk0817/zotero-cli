package resolve

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/yk0817/zotero-cli/zotero"
)

var (
	// metaTagRE matches a full <meta> tag, treating quoted attribute values as
	// opaque so a literal '>' inside content="…" does not end the tag early.
	metaTagRE  = regexp.MustCompile(`(?is)<meta\b(?:"[^"]*"|'[^']*'|[^>"'])*>`)
	attrRE     = regexp.MustCompile(`(?is)([a-zA-Z][a-zA-Z0-9:_.-]*)\s*=\s*("([^"]*)"|'([^']*)'|([^\s"'>]+))`)
	titleTagRE = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title>`)
)

// ResolveURL fetches a web page and resolves it from its embedded metadata: a
// scholarly page with Highwire citation_* tags becomes a journalArticle;
// anything else falls back to a webpage built from OpenGraph/<title>.
func (c *Client) ResolveURL(ctx context.Context, pageURL string) (zotero.ItemData, error) {
	pageURL = strings.TrimSpace(pageURL)
	if pageURL == "" {
		return zotero.ItemData{}, fmt.Errorf("empty URL")
	}

	body, err := c.get(ctx, pageURL, "text/html")
	if err != nil {
		return zotero.ItemData{}, err
	}
	meta := parseMeta(string(body))

	title := meta.first("citation_title", "dc.title", "og:title")
	if title == "" {
		title = meta.title
	}
	if title == "" {
		return zotero.ItemData{}, fmt.Errorf("no title found at %s (no citation_title, og:title, or <title>)", pageURL)
	}

	finalURL := meta.first("og:url")
	if finalURL == "" {
		finalURL = pageURL
	}

	data := zotero.ItemData{
		Title: title,
		URL:   finalURL,
		Date:  normalizeSlashDate(meta.first("citation_publication_date", "citation_date", "dc.date")),
	}

	doi := meta.first("citation_doi")
	journal := meta.first("citation_journal_title")
	if doi != "" || journal != "" {
		// A page advertising a DOI or journal is an article, so use the article
		// fields Zotero defines. Prefer citation_author, fall back to DC.creator.
		data.ItemType = "journalArticle"
		data.DOI = doi
		data.PublicationTitle = journal
		data.AbstractNote = meta.first("citation_abstract", "dc.description", "og:description")
		data.Creators = citationCreators(meta.all("citation_author"))
		if len(data.Creators) == 0 {
			data.Creators = citationCreators(meta.all("dc.creator"))
		}
	} else {
		// A webpage has neither a publicationTitle nor a DOI field — leaving
		// them empty keeps Zotero from rejecting the item as invalid.
		data.ItemType = "webpage"
		data.AbstractNote = meta.first("og:description", "dc.description", "description")
		data.Creators = citationCreators(meta.all("dc.creator"))
	}
	return data, nil
}

// pageMeta holds meta values keyed by lowercased name/property (values in
// document order so repeated tags like citation_author are preserved) plus the
// <title> text.
type pageMeta struct {
	values map[string][]string
	title  string
}

func parseMeta(htmlBody string) pageMeta {
	m := pageMeta{values: map[string][]string{}}
	for _, tag := range metaTagRE.FindAllString(htmlBody, -1) {
		attrs := parseAttrs(tag)
		key := attrs["name"]
		if key == "" {
			key = attrs["property"]
		}
		content, ok := attrs["content"]
		if key == "" || !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		m.values[key] = append(m.values[key], collapseWS(html.UnescapeString(content)))
	}
	if t := titleTagRE.FindStringSubmatch(htmlBody); t != nil {
		m.title = collapseWS(html.UnescapeString(t[1]))
	}
	return m
}

// parseAttrs extracts an HTML tag's attributes into a lowercased-key map,
// handling double-quoted, single-quoted, and unquoted values in any order.
func parseAttrs(tag string) map[string]string {
	attrs := map[string]string{}
	for _, match := range attrRE.FindAllStringSubmatch(tag, -1) {
		name := strings.ToLower(match[1])
		val := match[3]
		if val == "" {
			val = match[4]
		}
		if val == "" {
			val = match[5]
		}
		attrs[name] = val
	}
	return attrs
}

func (m pageMeta) first(keys ...string) string {
	for _, k := range keys {
		if v := m.values[k]; len(v) > 0 && v[0] != "" {
			return v[0]
		}
	}
	return ""
}

func (m pageMeta) all(key string) []string {
	return m.values[key]
}

// citationCreators parses author strings that may be "Last, First" (Highwire)
// or "First Last", returning author creators.
func citationCreators(names []string) []zotero.Creator {
	creators := make([]zotero.Creator, 0, len(names))
	for _, n := range names {
		n = collapseWS(n)
		if n == "" {
			continue
		}
		if i := strings.Index(n, ","); i >= 0 {
			creators = append(creators, zotero.Creator{
				CreatorType: "author",
				FirstName:   strings.TrimSpace(n[i+1:]),
				LastName:    strings.TrimSpace(n[:i]),
			})
			continue
		}
		creators = append(creators, personCreator(n))
	}
	return creators
}

// normalizeSlashDate turns a slash date (2016/06/27) into an ISO-style dash
// date so stored dates are consistent across sources.
func normalizeSlashDate(s string) string {
	return strings.ReplaceAll(collapseWS(s), "/", "-")
}

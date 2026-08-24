package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"html/template"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	siteBuildManifestName    = ".annolux-site-build.json"
	siteBuildManifestVersion = 1
)

type siteBuildManifest struct {
	Version       int    `json:"version"`
	Mode          string `json:"mode"`
	SourcesSHA256 string `json:"sources_sha256"`
	OutputSHA256  string `json:"output_sha256"`
}

type localized struct {
	EN string `json:"en"`
	ZH string `json:"zh"`
}

func (l localized) text(lang string) string {
	if lang == "zh" {
		return l.ZH
	}
	return l.EN
}

type sectionLink struct {
	Label localized `json:"label"`
	Slug  string    `json:"slug"`
}

type section struct {
	Heading    localized     `json:"heading"`
	Paragraphs []localized   `json:"paragraphs"`
	Bullets    []localized   `json:"bullets"`
	Code       string        `json:"code"`
	Links      []sectionLink `json:"links"`
}

type plan struct {
	Name    string    `json:"name"`
	Price   string    `json:"price"`
	Credits localized `json:"credits"`
	Summary localized `json:"summary"`
}

type page struct {
	Slug        string    `json:"slug"`
	Kind        string    `json:"kind"`
	NoIndex     bool      `json:"noindex"`
	Title       localized `json:"title"`
	Description localized `json:"description"`
	Kicker      localized `json:"kicker"`
	Sections    []section `json:"sections"`
	Plans       []plan    `json:"plans"`
	Published   string    `json:"published"`
}

type siteConfig struct {
	Name          string `json:"name"`
	Origin        string `json:"origin"`
	APIOrigin     string `json:"apiOrigin"`
	GitHub        string `json:"github"`
	SupportEmail  string `json:"supportEmail"`
	PrivacyEmail  string `json:"privacyEmail"`
	SecurityEmail string `json:"securityEmail"`
}

type content struct {
	Site     siteConfig                   `json:"site"`
	HomeCopy map[string]map[string]string `json:"homeCopy"`
	AuthCopy map[string]map[string]string `json:"authCopy"`
	Pages    []page                       `json:"pages"`
	Posts    []page                       `json:"posts"`
}

type renderedLink struct {
	Label, Href string
}

type renderedSection struct {
	Heading    string
	Paragraphs []template.HTML
	Bullets    []string
	Code       string
	Links      []renderedLink
}

type renderedPlan struct {
	Name, Price, Credits, Summary string
	ID, Href, CTA                 string
}

type renderedPost struct {
	Title, Description, Published, URL string
}

type docsNavItem struct {
	Label, Href string
	Current     bool
}

type docsNavGroup struct {
	Label string
	Items []docsNavItem
}

type pageView struct {
	Lang, HTMLLang, Title, Description, Kicker, Published string
	Canonical, EnglishURL, ChineseURL, AlternateURL       string
	HomeURL, DocsURL, PricingURL, BenchmarksURL, BlogURL  string
	McpURL, ChangelogURL                                  string
	StatusURL, ContactURL, PrivacyURL, TermsURL           string
	RefundURL, AUPURL, LoginURL                           string
	NoIndex, IsPost, IsAccount, IsCheckout, IsPlayground  bool
	IsBilling, IsStatus, IsDocs, IsPricing                bool
	NavDocs, NavMCP, NavPricing, NavBlog, NavBenchmarks   bool
	JSONLD                                                template.JS
	Sections                                              []renderedSection
	Plans                                                 []renderedPlan
	Posts                                                 []renderedPost
	DocsNav                                               []docsNavGroup
}

type legalDetails struct {
	Name, Country, GoverningLaw string
}

func main() {
	contentPath := flag.String("content", "website/content.json", "localized content dictionary")
	homePath := flag.String("home", "website/src/home.html", "home layout source")
	loginPath := flag.String("login", "website/src/login.html", "login layout source")
	templatePath := flag.String("template", "website/src/page.tmpl", "content page template")
	outPath := flag.String("out", "website/dist", "generated static site")
	release := flag.Bool("release", false, "require verified legal seller details")
	check := flag.Bool("check", false, "verify that the generated site matches its current sources")
	flag.Parse()
	sourcePaths := siteSourcePaths(*contentPath, *homePath, *loginPath, *templatePath)
	if *check {
		if err := checkBuildManifest(*outPath, sourcePaths, *release); err != nil {
			fmt.Fprintln(os.Stderr, "sitegen:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(*contentPath, *homePath, *loginPath, *templatePath, *outPath, *release); err != nil {
		fmt.Fprintln(os.Stderr, "sitegen:", err)
		os.Exit(1)
	}
}

func run(contentPath, homePath, loginPath, templatePath, outPath string, release bool) error {
	loaded, err := loadContent(contentPath)
	if err != nil {
		return err
	}
	legal, err := loadLegal(release)
	if err != nil {
		return err
	}
	pageTemplate, err := template.ParseFiles(templatePath)
	if err != nil {
		return fmt.Errorf("parse page template: %w", err)
	}
	absOut, err := filepath.Abs(outPath)
	if err != nil {
		return err
	}
	if filepath.Base(absOut) != "dist" {
		return errors.New("output directory must end in dist")
	}
	if err := os.RemoveAll(absOut); err != nil {
		return fmt.Errorf("clear generated site: %w", err)
	}
	if err := os.MkdirAll(absOut, 0o755); err != nil {
		return err
	}

	for _, lang := range []string{"en", "zh"} {
		if err := generateLayoutPage(homePath, absOut, "", lang, loaded, loaded.HomeCopy[lang], false); err != nil {
			return err
		}
		if err := generateLayoutPage(loginPath, absOut, "login", lang, loaded, loaded.AuthCopy[lang], true); err != nil {
			return err
		}
		for _, item := range loaded.Pages {
			view := makeView(loaded, item, lang, legal, false)
			if err := renderPage(pageTemplate, absOut, item.Slug, lang, view); err != nil {
				return err
			}
		}
		blog := page{Slug: "blog", Title: localized{EN: "Annolux Lab Notes", ZH: "Annolux Lab Notes"},
			Description: localized{EN: "Reproducible notes about search evaluation, index operations, evidence boundaries, and agent integration.", ZH: "关于搜索评测、索引运维、证据边界与 Agent 接入的可复现工程笔记。"},
			Kicker:      localized{EN: "ENGINEERING NOTES · NO CONTENT FARM", ZH: "工程笔记 · 不做内容农场"}}
		blogView := makeView(loaded, blog, lang, legal, false)
		for _, post := range loaded.Posts {
			blogView.Posts = append(blogView.Posts, renderedPost{Title: post.Title.text(lang), Description: post.Description.text(lang), Published: post.Published, URL: localPath(lang, "blog/"+post.Slug)})
		}
		if err := renderPage(pageTemplate, absOut, "blog", lang, blogView); err != nil {
			return err
		}
		for _, post := range loaded.Posts {
			post.Slug = "blog/" + post.Slug
			view := makeView(loaded, post, lang, legal, true)
			if err := renderPage(pageTemplate, absOut, post.Slug, lang, view); err != nil {
				return err
			}
		}
	}
	if err := generateNotFound(pageTemplate, absOut, loaded, legal); err != nil {
		return err
	}
	if err := copyPublicFiles(absOut); err != nil {
		return err
	}
	if err := writeMachineFiles(absOut, loaded); err != nil {
		return err
	}
	ver, err := assetVersion()
	if err != nil {
		return fmt.Errorf("compute asset version: %w", err)
	}
	if err := stampAssetVersions(absOut, ver); err != nil {
		return fmt.Errorf("stamp asset versions: %w", err)
	}
	return writeBuildManifest(absOut, siteSourcePaths(contentPath, homePath, loginPath, templatePath), release)
}

// assetVersion is a content hash of the CSS/JS the pages load. It changes only
// when one of those files changes, so every deploy gets a fresh query string
// while unchanged deploys stay byte-identical.
func assetVersion() (string, error) {
	names := append([]string(nil), publicFileNames()...)
	sort.Strings(names)
	hash := sha256.New()
	for _, name := range names {
		if !strings.HasSuffix(name, ".css") && !strings.HasSuffix(name, ".js") {
			continue
		}
		data, err := os.ReadFile(filepath.Join("website", name))
		if err != nil {
			return "", err
		}
		hash.Write([]byte(name))
		hash.Write([]byte{0})
		hash.Write(data)
	}
	return hex.EncodeToString(hash.Sum(nil))[:10], nil
}

// Root-relative CSS/JS references only; external hosts (https://…) and font
// files never match, so paddle and the woff2 assets are left alone.
var assetRefPattern = regexp.MustCompile(`(href|src)="(/[A-Za-z0-9._-]+\.(?:css|js))(?:\?[^"]*)?"`)

// stampAssetVersions rewrites every local CSS/JS URL in the generated HTML to
// carry ?v=<ver>. The HTML itself is served uncached, so a returning visitor
// always sees the current query string and the CDN treats a changed asset as a
// new URL instead of serving a stale cached copy.
func stampAssetVersions(out, ver string) error {
	replacement := []byte(`$1="$2?v=` + ver + `"`)
	return filepath.WalkDir(out, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		stamped := assetRefPattern.ReplaceAll(data, replacement)
		if bytes.Equal(stamped, data) {
			return nil
		}
		return os.WriteFile(path, stamped, 0o644)
	})
}

func loadContent(path string) (content, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return content{}, err
	}
	var loaded content
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&loaded); err != nil {
		return content{}, fmt.Errorf("decode content: %w", err)
	}
	if loaded.Site.Origin != "https://annolux.com" || loaded.Site.GitHub != "https://github.com/eason4kim-rocket/annolux" {
		return content{}, errors.New("content contains an unexpected public identity")
	}
	return loaded, nil
}

func loadLegal(release bool) (legalDetails, error) {
	details := legalDetails{
		Name: strings.TrimSpace(os.Getenv("ANNOLUX_LEGAL_NAME")), Country: strings.TrimSpace(os.Getenv("ANNOLUX_LEGAL_COUNTRY")),
		GoverningLaw: strings.TrimSpace(os.Getenv("ANNOLUX_GOVERNING_LAW")),
	}
	if release && (details.Name == "" || details.Country == "" || details.GoverningLaw == "") {
		return legalDetails{}, errors.New("WAITING_USER: release build requires ANNOLUX_LEGAL_NAME, ANNOLUX_LEGAL_COUNTRY, and ANNOLUX_GOVERNING_LAW confirmed from the verified seller profile")
	}
	if details.Name == "" {
		details.Name = "Eason Liu"
	}
	if details.Country == "" {
		details.Country = "Hong Kong"
	}
	if details.GoverningLaw == "" {
		details.GoverningLaw = "Hong Kong"
	}
	return details, nil
}

func makeView(loaded content, item page, lang string, legal legalDetails, isPost bool) pageView {
	canonical := loaded.Site.Origin + localPath(lang, item.Slug)
	view := pageView{
		Lang: lang, HTMLLang: map[string]string{"en": "en", "zh": "zh-CN"}[lang],
		Title: replaceLegal(item.Title.text(lang), legal), Description: replaceLegal(item.Description.text(lang), legal),
		Kicker: replaceLegal(item.Kicker.text(lang), legal), Published: item.Published, NoIndex: item.NoIndex, IsPost: isPost,
		IsAccount: item.Kind == "account", IsCheckout: item.Kind == "checkout", IsPlayground: item.Kind == "playground", IsBilling: item.Kind == "billing", IsStatus: item.Kind == "status",
		Canonical: canonical, EnglishURL: loaded.Site.Origin + localPath("en", item.Slug), ChineseURL: loaded.Site.Origin + localPath("zh", item.Slug),
		AlternateURL: localPath(otherLang(lang), item.Slug), HomeURL: localPath(lang, ""), DocsURL: localPath(lang, "docs"),
		PricingURL: localPath(lang, "pricing"), BenchmarksURL: localPath(lang, "benchmarks"), BlogURL: localPath(lang, "blog"),
		McpURL: localPath(lang, "mcp"), ChangelogURL: localPath(lang, "changelog"),
		StatusURL: localPath(lang, "status"), ContactURL: localPath(lang, "contact"), PrivacyURL: localPath(lang, "privacy"),
		TermsURL: localPath(lang, "terms"), RefundURL: localPath(lang, "refund"), AUPURL: localPath(lang, "acceptable-use"), LoginURL: localPath(lang, "login"),
		IsDocs: isDocsPage(item.Slug), IsPricing: item.Kind == "pricing" || item.Slug == "pricing",
		NavDocs: isDocsNavHighlight(item.Slug), NavMCP: isMCPNavHighlight(item.Slug),
		NavPricing: item.Slug == "pricing", NavBlog: isPost || item.Slug == "blog" || strings.HasPrefix(item.Slug, "blog/"),
		NavBenchmarks: item.Slug == "benchmarks",
	}
	if view.IsDocs {
		view.DocsNav = docsNav(lang, item.Slug)
	}
	for _, source := range item.Sections {
		section := renderedSection{Heading: replaceLegal(source.Heading.text(lang), legal), Code: source.Code}
		for _, value := range source.Paragraphs {
			section.Paragraphs = append(section.Paragraphs, linkifyPlain(replaceLegal(value.text(lang), legal)))
		}
		for _, value := range source.Bullets {
			section.Bullets = append(section.Bullets, replaceLegal(value.text(lang), legal))
		}
		for _, value := range source.Links {
			section.Links = append(section.Links, renderedLink{Label: value.Label.text(lang), Href: localPath(lang, value.Slug)})
		}
		view.Sections = append(view.Sections, section)
	}
	for _, source := range item.Plans {
		rendered := renderedPlan{Name: source.Name, Price: source.Price, Credits: source.Credits.text(lang), Summary: source.Summary.text(lang)}
		switch strings.ToLower(source.Name) {
		case "pro":
			rendered.ID = "pro"
			rendered.Href = localPath(lang, "billing/checkout") + "?plan=pro"
			rendered.CTA = map[string]string{"en": "Choose Pro", "zh": "选择 Pro"}[lang]
		case "scale":
			rendered.ID = "scale"
			rendered.Href = localPath(lang, "billing/checkout") + "?plan=scale"
			rendered.CTA = map[string]string{"en": "Choose Scale", "zh": "选择 Scale"}[lang]
		default:
			rendered.ID = "free"
			rendered.Href = view.LoginURL
			rendered.CTA = map[string]string{"en": "Start free", "zh": "免费开始"}[lang]
		}
		view.Plans = append(view.Plans, rendered)
	}
	view.JSONLD = jsonLD(loaded.Site, view)
	return view
}

func isDocsPage(slug string) bool {
	return slug == "docs" || slug == "mcp" || strings.HasPrefix(slug, "mcp/") || strings.HasPrefix(slug, "integrations/")
}

func isDocsNavHighlight(slug string) bool {
	return slug == "docs" || strings.HasPrefix(slug, "integrations/")
}

func isMCPNavHighlight(slug string) bool {
	return slug == "mcp" || strings.HasPrefix(slug, "mcp/")
}

func docsNav(lang, slug string) []docsNavGroup {
	item := func(en, zh, itemSlug string) docsNavItem {
		label := en
		if lang == "zh" {
			label = zh
		}
		return docsNavItem{Label: label, Href: localPath(lang, itemSlug), Current: slug == itemSlug}
	}
	start, clients, integrations := "Getting started", "MCP clients", "Integrations"
	if lang == "zh" {
		start, clients, integrations = "开始", "MCP 客户端", "组件接入"
	}
	return []docsNavGroup{
		{Label: start, Items: []docsNavItem{
			item("Search API", "API 文档", "docs"),
			item("MCP", "MCP", "mcp"),
		}},
		{Label: clients, Items: []docsNavItem{
			item("Claude Code", "Claude Code", "mcp/claude-code"),
			item("Claude Desktop", "Claude Desktop", "mcp/claude-desktop"),
			item("Cursor", "Cursor", "mcp/cursor"),
			item("Windsurf", "Windsurf", "mcp/windsurf"),
			item("Cline", "Cline", "mcp/cline"),
			item("Zed", "Zed", "mcp/zed"),
		}},
		{Label: integrations, Items: []docsNavItem{
			item("LangChain", "LangChain", "integrations/langchain"),
			item("LlamaIndex", "LlamaIndex", "integrations/llamaindex"),
			item("Dify", "Dify", "integrations/dify"),
		}},
	}
}

var emailPattern = regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@annolux\.com\b`)

func linkifyPlain(s string) template.HTML {
	escaped := html.EscapeString(s)
	return template.HTML(emailPattern.ReplaceAllStringFunc(escaped, func(addr string) string {
		return `<a href="mailto:` + addr + `">` + addr + `</a>`
	}))
}

func replaceLegal(value string, legal legalDetails) string {
	replacer := strings.NewReplacer("{{LEGAL_NAME}}", legal.Name, "{{LEGAL_COUNTRY}}", legal.Country, "{{GOVERNING_LAW}}", legal.GoverningLaw)
	return replacer.Replace(value)
}

func jsonLD(site siteConfig, view pageView) template.JS {
	object := map[string]any{"@context": "https://schema.org", "@type": "WebPage", "name": view.Title, "description": view.Description, "url": view.Canonical, "inLanguage": view.HTMLLang}
	if view.IsPost {
		post := map[string]any{
			"@type": "BlogPosting", "headline": view.Title, "description": view.Description, "url": view.Canonical,
			"datePublished": view.Published, "dateModified": view.Published, "inLanguage": view.HTMLLang,
			"author":    map[string]string{"@type": "Organization", "name": site.Name},
			"publisher": map[string]string{"@type": "Organization", "name": site.Name, "url": site.Origin},
			"isPartOf":  map[string]string{"@type": "Blog", "name": "Annolux Lab Notes", "url": site.Origin + localPath(view.Lang, "blog")},
		}
		breadcrumb := map[string]any{
			"@type": "BreadcrumbList",
			"itemListElement": []map[string]any{
				{"@type": "ListItem", "position": 1, "name": "Annolux", "item": site.Origin + localPath(view.Lang, "")},
				{"@type": "ListItem", "position": 2, "name": "Annolux Lab Notes", "item": site.Origin + localPath(view.Lang, "blog")},
				{"@type": "ListItem", "position": 3, "name": view.Title, "item": view.Canonical},
			},
		}
		object = map[string]any{"@context": "https://schema.org", "@graph": []any{post, breadcrumb}}
	}
	encoded, _ := json.Marshal(object)
	return template.JS(encoded)
}

func renderPage(pageTemplate *template.Template, out, slug, lang string, view pageView) error {
	path := outputPath(out, lang, slug)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var rendered bytes.Buffer
	if err := pageTemplate.ExecuteTemplate(&rendered, filepath.Base(pageTemplate.Name()), view); err != nil {
		return fmt.Errorf("render %s %s: %w", lang, slug, err)
	}
	return os.WriteFile(path, rendered.Bytes(), 0o644)
}

func generateLayoutPage(sourcePath, out, slug, lang string, loaded content, dictionary map[string]string, auth bool) error {
	file, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer file.Close()
	doc, err := nethtml.Parse(file)
	if err != nil {
		return err
	}
	removeClasses := map[string]bool{}
	var walk func(*nethtml.Node)
	walk = func(node *nethtml.Node) {
		for child := node.FirstChild; child != nil; {
			next := child.NextSibling
			if hasAnyClass(child, removeClasses) {
				node.RemoveChild(child)
			} else {
				walk(child)
			}
			child = next
		}
		if node.Type != nethtml.ElementNode {
			return
		}
		keyName := "data-copy"
		if auth {
			keyName = "data-auth-copy"
		}
		if key := attr(node, keyName); key != "" {
			if value, ok := dictionary[key]; ok {
				setInnerHTML(node, value)
			}
			removeAttr(node, keyName)
		}
	}
	walk(doc)
	setHTMLAttr(doc, "lang", map[string]string{"en": "en", "zh": "zh-CN"}[lang])
	if auth {
		configureLogin(doc, lang, loaded)
	} else {
		configureHome(doc, lang, loaded)
	}
	path := outputPath(out, lang, slug)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var rendered bytes.Buffer
	if err := nethtml.Render(&rendered, doc); err != nil {
		return err
	}
	return os.WriteFile(path, rendered.Bytes(), 0o644)
}

func configureHome(doc *nethtml.Node, lang string, loaded content) {
	title := map[string]string{"en": "Bilingual Search API for AI Agents & RAG — Annolux", "zh": "Annolux Search — 面向 AI Agent 的中英双语搜索 API 与 MCP"}[lang]
	description := map[string]string{"en": "A focused English and Chinese Search API for AI agents and RAG systems. Clean source-attributed snippets, explicit fetched_at timestamps, and deterministic credit billing.", "zh": "面向 Agent 的中英双语精选 Search API，返回带来源归属和明确抓取时间的结果。智能理解世界的方式。"}[lang]
	canonical := loaded.Site.Origin + localPath(lang, "")
	configureMetadata(doc, title, description, canonical, loaded.Site.Origin+"/", loaded.Site.Origin+"/zh/")
	if lang == "zh" {
		// The Chinese homepage renders in the heavy Noto Sans SC subset; the
		// English page never fetches it. Preload it on the zh homepage only so
		// the ~200 KB face downloads in parallel with the CSS instead of after
		// it, cutting first paint. The URL must match home.css's @font-face src.
		preloadHead(doc, "/assets/fonts/NotoSansSC-AnnoluxSubset.woff2?v=site-p1-812")
	}
	visitElements(doc, func(node *nethtml.Node) {
		if node.Data == "script" && attr(node, "type") == "application/ld+json" {
			setText(node, `{"@context":"https://schema.org","@graph":[{"@type":"Organization","name":"Annolux","url":"https://annolux.com/","logo":"https://annolux.com/favicon.svg"},{"@type":"WebSite","name":"Annolux","url":"https://annolux.com/","inLanguage":["en","zh-CN"]}]}`)
		}
		configureLink(node, lang, loaded.Site)
		if hasAttr(node, "data-language-toggle") || hasClass(node, "language-toggle") || hasClass(node, "lang-toggle") {
			node.Data = "a"
			node.DataAtom = atom.A
			removeAttr(node, "type")
			removeAttr(node, "data-language-toggle")
			setAttr(node, "href", localPath(otherLang(lang), ""))
			setLanguagePill(node, lang)
			// Keep the visible "EN / 中文" text as the accessible name. An
			// overriding label that omits the visible text fails WCAG 2.5.3.
			removeAttr(node, "aria-label")
		}
	})
}

// preloadHead inserts a font preload link as the first child of <head> so the
// preload scanner starts the fetch before the stylesheet is parsed.
func preloadHead(doc *nethtml.Node, href string) {
	var head *nethtml.Node
	var find func(*nethtml.Node)
	find = func(node *nethtml.Node) {
		if head != nil {
			return
		}
		if node.Type == nethtml.ElementNode && node.DataAtom == atom.Head {
			head = node
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			find(child)
		}
	}
	find(doc)
	if head == nil {
		return
	}
	link := &nethtml.Node{
		Type:     nethtml.ElementNode,
		DataAtom: atom.Link,
		Data:     "link",
		Attr: []nethtml.Attribute{
			{Key: "rel", Val: "preload"},
			{Key: "href", Val: href},
			{Key: "as", Val: "font"},
			{Key: "type", Val: "font/woff2"},
			{Key: "crossorigin", Val: ""},
		},
	}
	head.InsertBefore(link, head.FirstChild)
}

// setLanguagePill renders the language switch so the page's own language reads
// as current and the other one as the destination. Both locales share one
// source template, so the order is decided here rather than in the markup.
func setLanguagePill(node *nethtml.Node, lang string) {
	current, next := "EN", "中文"
	if lang == "zh" {
		current, next = "中文", "EN"
	}
	for node.FirstChild != nil {
		node.RemoveChild(node.FirstChild)
	}
	node.AppendChild(pillSpan("lang-current", current, false))
	node.AppendChild(pillSpan("lang-divider", "/", true))
	node.AppendChild(pillSpan("lang-next", next, false))
}

func pillSpan(class, text string, decorative bool) *nethtml.Node {
	span := &nethtml.Node{Type: nethtml.ElementNode, DataAtom: atom.Span, Data: "span"}
	span.Attr = append(span.Attr, nethtml.Attribute{Key: "class", Val: class})
	if decorative {
		span.Attr = append(span.Attr, nethtml.Attribute{Key: "aria-hidden", Val: "true"})
	}
	span.AppendChild(&nethtml.Node{Type: nethtml.TextNode, Data: text})
	return span
}

func configureLogin(doc *nethtml.Node, lang string, loaded content) {
	title := map[string]string{"en": "Sign in — Annolux", "zh": "登录 — Annolux"}[lang]
	description := map[string]string{"en": "Sign in to Annolux with Google or GitHub.", "zh": "使用 Google 或 GitHub 登录 Annolux。"}[lang]
	configureMetadata(doc, title, description, loaded.Site.Origin+localPath(lang, "login"), loaded.Site.Origin+"/login", loaded.Site.Origin+"/zh/login")
	visitElements(doc, func(node *nethtml.Node) {
		configureLink(node, lang, loaded.Site)
		if hasAttr(node, "data-language-link") {
			setAttr(node, "href", localPath(otherLang(lang), "login"))
			setLanguagePill(node, lang)
			// The visible "EN / 中文" text is the accessible name; an overriding
			// label that omits it fails WCAG 2.5.3.
			removeAttr(node, "aria-label")
		}
	})
}

func configureMetadata(doc *nethtml.Node, title, description, canonical, englishURL, chineseURL string) {
	visitElements(doc, func(node *nethtml.Node) {
		switch node.Data {
		case "title":
			setText(node, title)
		case "meta":
			switch {
			case attr(node, "name") == "description":
				setAttr(node, "content", description)
			case attr(node, "property") == "og:title":
				setAttr(node, "content", title)
			case attr(node, "property") == "og:description":
				setAttr(node, "content", description)
			case attr(node, "property") == "og:url":
				setAttr(node, "content", canonical)
			case attr(node, "name") == "twitter:title":
				setAttr(node, "content", title)
			case attr(node, "name") == "twitter:description":
				setAttr(node, "content", description)
			}
		case "link":
			if attr(node, "rel") == "canonical" {
				setAttr(node, "href", canonical)
			}
			if attr(node, "rel") == "alternate" {
				switch attr(node, "hreflang") {
				case "en", "x-default":
					setAttr(node, "href", englishURL)
				case "zh-CN":
					setAttr(node, "href", chineseURL)
				}
			}
		}
		removeAttr(node, "data-content-en")
		removeAttr(node, "data-content-zh")
		removeAttr(node, "data-title-en")
		removeAttr(node, "data-title-zh")
	})
}

func configureLink(node *nethtml.Node, lang string, site siteConfig) {
	if node.Type != nethtml.ElementNode {
		return
	}
	if node.Data == "link" {
		href := attr(node, "href")
		if strings.Contains(href, "styles.css") {
			setAttr(node, "href", "/styles.css")
		}
		if strings.Contains(href, "favicon.svg") {
			setAttr(node, "href", "/favicon.svg")
		}
		if strings.Contains(href, "favicon.ico") {
			setAttr(node, "href", "/favicon.ico")
		}
		if strings.Contains(href, "favicon-96x96.png") {
			setAttr(node, "href", "/favicon-96x96.png")
		}
		if strings.Contains(href, "apple-touch-icon.png") {
			setAttr(node, "href", "/apple-touch-icon.png")
		}
		if strings.Contains(href, "assets/fonts") {
			setAttr(node, "href", "/assets/fonts/"+filepath.Base(href))
		}
	}
	if node.Data == "script" && strings.Contains(attr(node, "src"), "app.js") {
		setAttr(node, "src", "/app.js")
	}
	if node.Data != "a" {
		return
	}
	href := attr(node, "href")
	if strings.Contains(href, "github.com/eason4kim-rocket/annolux/issues") {
		setAttr(node, "href", localPath(lang, "login"))
		removeAttr(node, "target")
		removeAttr(node, "rel")
		return
	}
	if strings.HasPrefix(href, "./") || strings.HasPrefix(href, "/") {
		setAttr(node, "href", localizeInternal(href, lang))
		removeAttr(node, "target")
		removeAttr(node, "rel")
	}
}

func localizeInternal(raw, lang string) string {
	if strings.HasPrefix(raw, "/api/") || strings.HasPrefix(raw, "/assets/") {
		return raw
	}
	hash := ""
	if index := strings.IndexByte(raw, '#'); index >= 0 {
		hash = raw[index:]
		raw = raw[:index]
	}
	query := ""
	if index := strings.IndexByte(raw, '?'); index >= 0 {
		query = raw[index:]
		raw = raw[:index]
	}
	raw = strings.TrimPrefix(raw, "./")
	raw = strings.TrimPrefix(raw, "/")
	raw = strings.TrimSuffix(raw, ".html")
	if raw == "index" {
		raw = ""
	}
	return localPath(lang, raw) + query + hash
}

func generateNotFound(pageTemplate *template.Template, out string, loaded content, legal legalDetails) error {
	for _, lang := range []string{"en", "zh"} {
		item := page{Slug: "404", NoIndex: true,
			Title:       localized{EN: "Page not found", ZH: "页面不存在"},
			Description: localized{EN: "The requested URL does not exist.", ZH: "请求的网址不存在。"},
			Kicker:      localized{EN: "404 · NOT IN THE INDEX", ZH: "404 · 不在索引中"},
			Sections:    []section{{Heading: localized{EN: "Try a known route", ZH: "请访问已有页面"}, Paragraphs: []localized{{EN: "Return home, read the documentation, or contact support if a valid link is broken.", ZH: "请返回首页、阅读文档；若有效链接损坏，请联系支持。"}}}},
		}
		view := makeView(loaded, item, lang, legal, false)
		path := filepath.Join(out, "404.html")
		if lang == "zh" {
			path = filepath.Join(out, "zh", "404.html")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		var rendered bytes.Buffer
		if err := pageTemplate.ExecuteTemplate(&rendered, filepath.Base(pageTemplate.Name()), view); err != nil {
			return err
		}
		if err := os.WriteFile(path, rendered.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func publicFileNames() []string {
	return []string{"styles.css", "home.css", "pages.css", "theme-init.js", "app.js", "page.js", "auth.css", "auth.js", "account.js", "billing.js", "checkout.js", "playground.js", "favicon.svg", "favicon.ico", "favicon-96x96.png", "apple-touch-icon.png", "og.png", "llms.txt", "87402b19bc2e08fc27fd61efe4998651e918007a46d537e2db90c2c721b6bfad.txt"}
}

func copyPublicFiles(out string) error {
	for _, name := range publicFileNames() {
		if err := copyFile(filepath.Join("website", name), filepath.Join(out, name)); err != nil {
			return err
		}
	}
	if err := copyTree(filepath.Join("website", "assets"), filepath.Join(out, "assets")); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join("website", ".well-known")); err == nil {
		if err := copyTree(filepath.Join("website", ".well-known"), filepath.Join(out, ".well-known")); err != nil {
			return err
		}
	}
	return copyFile(filepath.Join("openapi", "annolux.openapi.json"), filepath.Join(out, "openapi.json"))
}

func siteSourcePaths(contentPath, homePath, loginPath, templatePath string) []string {
	paths := []string{
		contentPath,
		homePath,
		loginPath,
		templatePath,
		"go.mod",
		"go.sum",
		filepath.Join("openapi", "annolux.openapi.json"),
		filepath.Join("tools", "sitegen"),
		filepath.Join("website", "assets"),
	}
	for _, name := range publicFileNames() {
		paths = append(paths, filepath.Join("website", name))
	}
	return paths
}

type digestEntry struct {
	path  string
	label string
}

func collectDigestEntries(paths []string, labelBase string, excludedBaseName string) ([]digestEntry, error) {
	base, err := filepath.Abs(labelBase)
	if err != nil {
		return nil, err
	}
	entriesByLabel := make(map[string]digestEntry)
	for _, input := range paths {
		root, err := filepath.Abs(input)
		if err != nil {
			return nil, err
		}
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("site build input must not be a symlink: %s", path)
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("site build input must be a regular file: %s", path)
			}
			if excludedBaseName != "" && filepath.Base(path) == excludedBaseName {
				return nil
			}
			label, err := filepath.Rel(base, path)
			if err != nil {
				return err
			}
			label = filepath.ToSlash(label)
			if previous, exists := entriesByLabel[label]; exists && previous.path != path {
				return fmt.Errorf("duplicate site build input label %q", label)
			}
			entriesByLabel[label] = digestEntry{path: path, label: label}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	entries := make([]digestEntry, 0, len(entriesByLabel))
	for _, entry := range entriesByLabel {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].label < entries[j].label })
	return entries, nil
}

func digestEntries(entries []digestEntry) (string, error) {
	digest := sha256.New()
	for _, entry := range entries {
		if _, err := io.WriteString(digest, entry.label); err != nil {
			return "", err
		}
		if _, err := digest.Write([]byte{0}); err != nil {
			return "", err
		}
		file, err := os.Open(entry.path)
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(digest, file)
		closeErr := file.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		if _, err := digest.Write([]byte{0}); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func digestSourcePaths(paths []string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	entries, err := collectDigestEntries(paths, cwd, "")
	if err != nil {
		return "", err
	}
	return digestEntries(entries)
}

func digestSiteOutput(out string) (string, error) {
	entries, err := collectDigestEntries([]string{out}, out, siteBuildManifestName)
	if err != nil {
		return "", err
	}
	return digestEntries(entries)
}

func writeBuildManifest(out string, sourcePaths []string, release bool) error {
	sourcesDigest, err := digestSourcePaths(sourcePaths)
	if err != nil {
		return fmt.Errorf("hash site sources: %w", err)
	}
	outputDigest, err := digestSiteOutput(out)
	if err != nil {
		return fmt.Errorf("hash generated site: %w", err)
	}
	mode := "development"
	if release {
		mode = "release"
	}
	manifest := siteBuildManifest{Version: siteBuildManifestVersion, Mode: mode, SourcesSHA256: sourcesDigest, OutputSHA256: outputDigest}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(out, siteBuildManifestName), data, 0o644); err != nil {
		return fmt.Errorf("write site build manifest: %w", err)
	}
	return nil
}

func checkBuildManifest(out string, sourcePaths []string, requireRelease bool) error {
	data, err := os.ReadFile(filepath.Join(out, siteBuildManifestName))
	if err != nil {
		return fmt.Errorf("generated site manifest missing; run go run ./tools/sitegen: %w", err)
	}
	var manifest siteBuildManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return fmt.Errorf("decode generated site manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("generated site manifest contains trailing data")
	}
	if manifest.Version != siteBuildManifestVersion {
		return fmt.Errorf("generated site manifest version is %d, expected %d", manifest.Version, siteBuildManifestVersion)
	}
	if manifest.Mode != "development" && manifest.Mode != "release" {
		return fmt.Errorf("generated site manifest has invalid mode %q", manifest.Mode)
	}
	if requireRelease && manifest.Mode != "release" {
		return errors.New("generated site is not a release build; run go run ./tools/sitegen --release with verified legal details")
	}
	sourcesDigest, err := digestSourcePaths(sourcePaths)
	if err != nil {
		return fmt.Errorf("hash site sources: %w", err)
	}
	if manifest.SourcesSHA256 != sourcesDigest {
		return errors.New("generated site is stale: source files changed after generation")
	}
	outputDigest, err := digestSiteOutput(out)
	if err != nil {
		return fmt.Errorf("hash generated site: %w", err)
	}
	if manifest.OutputSHA256 != outputDigest {
		return errors.New("generated site is stale: generated files changed after generation")
	}
	return nil
}

func writeMachineFiles(out string, loaded content) error {
	routes := []string{"", "docs", "pricing", "benchmarks", "blog", "changelog", "about", "contact", "crawler", "security", "status", "privacy", "terms", "refund", "acceptable-use", "mcp", "mcp/claude-code", "mcp/claude-desktop", "mcp/cursor", "mcp/windsurf", "mcp/cline", "mcp/zed", "integrations/langchain", "integrations/llamaindex", "integrations/dify"}
	for _, post := range loaded.Posts {
		routes = append(routes, "blog/"+post.Slug)
	}
	sort.Strings(routes)
	var sitemap strings.Builder
	sitemap.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">` + "\n")
	for _, route := range routes {
		en := loaded.Site.Origin + localPath("en", route)
		zh := loaded.Site.Origin + localPath("zh", route)
		fmt.Fprintf(&sitemap, "  <url><loc>%s</loc><xhtml:link rel=\"alternate\" hreflang=\"en\" href=\"%s\"/><xhtml:link rel=\"alternate\" hreflang=\"zh-CN\" href=\"%s\"/><xhtml:link rel=\"alternate\" hreflang=\"x-default\" href=\"%s\"/></url>\n", en, en, zh, en)
		fmt.Fprintf(&sitemap, "  <url><loc>%s</loc><xhtml:link rel=\"alternate\" hreflang=\"en\" href=\"%s\"/><xhtml:link rel=\"alternate\" hreflang=\"zh-CN\" href=\"%s\"/><xhtml:link rel=\"alternate\" hreflang=\"x-default\" href=\"%s\"/></url>\n", zh, en, zh, en)
	}
	sitemap.WriteString("</urlset>\n")
	if err := os.WriteFile(filepath.Join(out, "sitemap.xml"), []byte(sitemap.String()), 0o644); err != nil {
		return err
	}
	robots := "User-agent: Googlebot\nAllow: /\nUser-agent: Bingbot\nAllow: /\nUser-agent: OAI-SearchBot\nAllow: /\nUser-agent: GPTBot\nDisallow: /\nUser-agent: Google-Extended\nDisallow: /\nUser-agent: CCBot\nDisallow: /\nUser-agent: *\nAllow: /\nDisallow: /account\nDisallow: /zh/account\nDisallow: /login\nDisallow: /zh/login\nDisallow: /playground\nDisallow: /zh/playground\nDisallow: /billing/\nDisallow: /zh/billing/\nDisallow: /checkout/\nDisallow: /zh/checkout/\nSitemap: https://annolux.com/sitemap.xml\n"
	if err := os.WriteFile(filepath.Join(out, "robots.txt"), []byte(robots), 0o644); err != nil {
		return err
	}
	var rss strings.Builder
	rss.WriteString(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>Annolux Lab Notes</title><link>https://annolux.com/blog</link><description>Reproducible search engineering notes.</description>`)
	for _, post := range loaded.Posts {
		published, err := time.Parse("2006-01-02", post.Published)
		if err != nil {
			return fmt.Errorf("invalid post date %q: %w", post.Published, err)
		}
		fmt.Fprintf(&rss, "<item><title>%s</title><link>%s</link><guid>%s</guid><pubDate>%s</pubDate><description>%s</description></item>", xmlEscape(post.Title.EN), loaded.Site.Origin+localPath("en", "blog/"+post.Slug), loaded.Site.Origin+localPath("en", "blog/"+post.Slug), published.UTC().Format(time.RFC1123Z), xmlEscape(post.Description.EN))
	}
	rss.WriteString("</channel></rss>\n")
	if err := os.WriteFile(filepath.Join(out, "feed.xml"), []byte(rss.String()), 0o644); err != nil {
		return err
	}
	security := "Contact: mailto:security@annolux.com\nCanonical: https://annolux.com/.well-known/security.txt\nPolicy: https://annolux.com/security\nPreferred-Languages: en, zh\nExpires: 2027-08-16T00:00:00Z\n"
	wellKnown := filepath.Join(out, ".well-known")
	if err := os.MkdirAll(wellKnown, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(wellKnown, "security.txt"), []byte(security), 0o644)
}

func outputPath(out, lang, slug string) string {
	parts := []string{out}
	if lang == "zh" {
		parts = append(parts, "zh")
	}
	if slug != "" {
		parts = append(parts, filepath.FromSlash(slug))
	}
	parts = append(parts, "index.html")
	return filepath.Join(parts...)
}

func localPath(lang, slug string) string {
	prefix := ""
	if lang == "zh" {
		prefix = "/zh"
	}
	slug = strings.Trim(slug, "/")
	if slug == "" {
		return prefix + "/"
	}
	return prefix + "/" + slug
}

func otherLang(lang string) string {
	if lang == "zh" {
		return "en"
	}
	return "zh"
}

func visitElements(node *nethtml.Node, fn func(*nethtml.Node)) {
	if node.Type == nethtml.ElementNode {
		fn(node)
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		visitElements(child, fn)
	}
}

func hasAnyClass(node *nethtml.Node, values map[string]bool) bool {
	for _, class := range strings.Fields(attr(node, "class")) {
		if values[class] {
			return true
		}
	}
	return false
}
func hasClass(node *nethtml.Node, value string) bool {
	for _, class := range strings.Fields(attr(node, "class")) {
		if class == value {
			return true
		}
	}
	return false
}
func hasAttr(node *nethtml.Node, key string) bool {
	for _, value := range node.Attr {
		if value.Key == key {
			return true
		}
	}
	return false
}
func attr(node *nethtml.Node, key string) string {
	for _, value := range node.Attr {
		if value.Key == key {
			return value.Val
		}
	}
	return ""
}
func setAttr(node *nethtml.Node, key, value string) {
	for i := range node.Attr {
		if node.Attr[i].Key == key {
			node.Attr[i].Val = value
			return
		}
	}
	node.Attr = append(node.Attr, nethtml.Attribute{Key: key, Val: value})
}
func removeAttr(node *nethtml.Node, key string) {
	kept := node.Attr[:0]
	for _, value := range node.Attr {
		if value.Key != key {
			kept = append(kept, value)
		}
	}
	node.Attr = kept
}
func setHTMLAttr(node *nethtml.Node, key, value string) {
	if node.Type == nethtml.ElementNode && node.Data == "html" {
		setAttr(node, key, value)
		return
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		setHTMLAttr(child, key, value)
	}
}
func setText(node *nethtml.Node, value string) {
	for node.FirstChild != nil {
		node.RemoveChild(node.FirstChild)
	}
	node.AppendChild(&nethtml.Node{Type: nethtml.TextNode, Data: value})
}
func setInnerHTML(node *nethtml.Node, value string) {
	for node.FirstChild != nil {
		node.RemoveChild(node.FirstChild)
	}
	children, err := nethtml.ParseFragment(strings.NewReader(value), node)
	if err != nil {
		setText(node, value)
		return
	}
	for _, child := range children {
		node.AppendChild(child)
	}
}

func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		return copyFile(path, destination)
	})
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return replacer.Replace(value)
}

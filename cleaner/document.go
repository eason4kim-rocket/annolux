package cleaner

import "github.com/eason4kim-rocket/annolux/quality"

// Document is the indexer's normalized representation of one fetched page.
// It is internal processing data, not a customer-facing scrape response.
type Document struct {
	Content    string
	Metadata   Metadata
	Links      LinksResult
	Images     []Image
	OGMetadata OGMetadata
	Tokens     TokenInfo
	Quality    *quality.Info
}

type LinksResult struct {
	Internal []Link
	External []Link
}

type Link struct {
	Href string
	Text string
}

type Image struct {
	Src string
	Alt string
}

type OGMetadata struct {
	Title       string
	Description string
	Image       string
	Type        string
}

type Metadata struct {
	Title       string
	Description string
	SiteName    string
	Author      string
	Language    string
	SourceURL   string
}

type TokenInfo struct {
	OriginalEstimate int
	CleanedEstimate  int
	SavingsPercent   float64
}

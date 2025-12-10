package extra

import "github.com/GrailFinder/searchagent/searcher"

var WebSearcher searcher.WebSurfer

func init() {
	sa, err := searcher.NewWebSurfer(searcher.SearcherTypeScraper, "")
	if err != nil {
		panic("failed to init seachagent; error: " + err.Error())
	}
	WebSearcher = sa
}

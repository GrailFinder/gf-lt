package extra

import "github.com/GrailFinder/searchagent/searcher"

var WebSearcher searcher.Searcher

func init() {
	sa, err := searcher.NewSearchService(searcher.SearcherTypeScraper, "")
	if err != nil {
		panic("failed to init seachagent; error: " + err.Error())
	}
	WebSearcher = sa
}

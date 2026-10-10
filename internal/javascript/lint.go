package javascript

import (
	"context"
	"fmt"
)

type lintCursor struct {
	PathIndex   int `json:"pathIndex"`
	ResultIndex int `json:"resultIndex"`
}

func (bundle Bundle) lintPages(ctx context.Context, payload request) (LintResult, error) {
	payload.Cursor = &lintCursor{}
	result := LintResult{}
	for {
		page, err := bundle.lintPage(ctx, payload)
		if err != nil {
			return LintResult{}, err
		}
		result.Findings = append(result.Findings, page.Findings...)
		result.Comments = append(result.Comments, page.Comments...)
		result.Unsupported = append(result.Unsupported, page.Unsupported...)
		if page.next == nil {
			return result, nil
		}
		payload.Cursor = page.next
	}
}

func (bundle Bundle) lintPage(ctx context.Context, payload request) (LintResult, error) {
	data, err := bundle.exchange(ctx, payload, lintTimeout)
	if err != nil {
		return LintResult{}, err
	}
	page, err := decodeLintResult(data)
	if err != nil {
		return LintResult{}, fmt.Errorf("the sealed JavaScript bundle returned an unreadable lint result: %w", err)
	}
	if page.next != nil && !page.next.follows(*payload.Cursor, len(payload.Paths)) {
		return LintResult{}, fmt.Errorf("the sealed JavaScript bundle returned a lint cursor that does not advance within the selected paths")
	}
	return page, nil
}

func (cursor lintCursor) follows(previous lintCursor, paths int) bool {
	if cursor.PathIndex < 0 || cursor.PathIndex >= paths || cursor.ResultIndex < 0 {
		return false
	}
	return cursor.PathIndex > previous.PathIndex || cursor.PathIndex == previous.PathIndex && cursor.ResultIndex > previous.ResultIndex
}
